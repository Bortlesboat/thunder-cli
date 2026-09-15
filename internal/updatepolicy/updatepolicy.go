package updatepolicy

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
)

// debugf prints diagnostics when TNR_UPDATE_DEBUG=1
func debugf(format string, args ...any) {
	if os.Getenv("TNR_UPDATE_DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, "[tnr-update] "+format+"\n", args...)
	}
}

const (
	cacheDirName           = "tnr"
	minVersionCacheName    = "min_version.json"
	manifestCacheName      = "latest_manifest.json"
	checksumsCacheTemplate = "checksums_%s_%s.json" // version, os

	cacheTTL = 24 * time.Hour

	// Failures to resolve a checksum are cached far more briefly than successes, so
	// a release that republishes checksums.txt is picked up without a day's wait.
	negativeChecksumCacheTTL = 1 * time.Hour

	minVersionEnvKey = "TNR_MIN_VERSION_URL"
)

var githubReleasesLatestURL = "https://api.github.com/repos/Thunder-Compute/thunder-cli/releases/latest"
var githubReleaseDownloadBaseURL = "https://github.com/Thunder-Compute/thunder-cli/releases/download"

// setGitHubReleasesURL overrides the GitHub API URL (for testing).
func setGitHubReleasesURL(url string) { githubReleasesLatestURL = url }

// Result captures the outcome of the update policy evaluation.
type Result struct {
	CurrentVersion string
	LatestVersion  string
	LatestTag      string
	MinVersion     string

	Mandatory bool
	Optional  bool
	Reason    string

	AssetURL       string
	ChecksumURL    string
	ExpectedSHA256 string

	// ChecksumUnavailable reports that an update exists but its checksum could not
	// be resolved, so the update must not be installed. ChecksumError carries why.
	// Whether an update *exists* is deliberately not conditioned on this — callers
	// still get to tell the user about the new version and point them at a manual
	// download. autoupdate.PerformUpdate refuses an unverifiable archive on its own.
	ChecksumUnavailable bool
	ChecksumError       error
}

// Check evaluates whether the CLI needs to update, using cached metadata when possible.
// If force is true, cached values are ignored and fresh network calls are made.
func Check(ctx context.Context, currentVersion string, force bool) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	res := Result{
		CurrentVersion: normalizeVersion(currentVersion),
	}

	if strings.EqualFold(res.CurrentVersion, "dev") || res.CurrentVersion == "" {
		// Development builds skip update checks entirely.
		return res, nil
	}

	manifest, err := fetchLatestManifest(ctx, force)
	if err != nil {
		return res, fmt.Errorf("fetch latest manifest: %w", err)
	}
	res.LatestTag = manifest.Version
	res.LatestVersion = normalizeVersion(manifest.Version)

	platform := detectPlatform()
	assetURL, checksumURL := manifest.AssetFor(platform)

	// GitHub fallback for asset/checksum when manifest did not include them
	if assetURL == "" || checksumURL == "" {
		ghAsset, ghChecksum := githubAssetAndChecksum(manifest.Version, platform)
		if assetURL == "" {
			assetURL = ghAsset
		}
		if checksumURL == "" {
			checksumURL = ghChecksum
		}
	}

	res.AssetURL = assetURL
	debugf("asset: %s", res.AssetURL)

	// Decide minimum CLI version
	if url := resolveMinVersionURL(); url != "" {
		if minVersion, err := fetchMinVersion(ctx, force); err == nil {
			res.MinVersion = normalizeVersion(minVersion)
			debugf("min-version: using %s from %s", res.MinVersion, url)
		} else if force {
			return res, fmt.Errorf("fetch min version: %w", err)
		}
	} else {
		// No min-version source configured, so nothing is mandatory. Defaulting
		// MinVersion to LatestVersion here made *every* version behind latest a
		// forced update — and a forced update the CLI cannot complete exits(1),
		// which turns any fault in the update path into an unusable CLI. Forced
		// updates stay opt-in via TNR_MIN_VERSION_URL, as documented.
		debugf("min-version: not configured (%s unset); updates are optional", minVersionEnvKey)
	}

	currentSemver, errCur := semver.NewVersion(res.CurrentVersion)
	if errCur != nil {
		return res, fmt.Errorf("parse current version: %w", errCur)
	}

	if res.MinVersion != "" {
		minSemver, errMin := semver.NewVersion(res.MinVersion)
		if errMin == nil && currentSemver.LessThan(minSemver) {
			res.Mandatory = true
			res.Optional = false
			res.Reason = "min-version"
		}
	}

	if !res.Mandatory && res.LatestVersion != "" {
		latestSemver, errLatest := semver.NewVersion(res.LatestVersion)
		if errLatest == nil && currentSemver.LessThan(latestSemver) {
			res.Optional = true
			res.Reason = "new-version"
		}
	}

	// Resolve the checksum only once an update is actually on the table. It costs a
	// network round-trip — several, and up to httpTimeout each when they fail — and
	// a CLI that is already current never installs anything, so paying for it on
	// every command buys nothing.
	if res.Mandatory || res.Optional {
		resolveChecksum(ctx, &res, manifest.Version, platform, checksumURL, force)
	}

	return res, nil
}

// resolveChecksum populates the expected archive checksum for an available update,
// recording failure on the Result instead of returning an error. See the
// ChecksumUnavailable field for why this does not fail the check.
func resolveChecksum(ctx context.Context, res *Result, version string, p platform, manifestChecksumURL string, force bool) {
	candidates := checksumCandidates(manifestChecksumURL, res.AssetURL, version, p.OS)
	// Always include the GitHub OS-specific checksum as an additional candidate
	if _, ghChecksum := githubAssetAndChecksum(version, p); ghChecksum != "" {
		candidates = append(candidates, ghChecksum)
	}

	expectedChecksum, usedURL, err := fetchChecksum(ctx, version, p.OS, candidates, force)
	if err != nil {
		res.ChecksumUnavailable = true
		res.ChecksumError = err
		debugf("checksums: unavailable (%v)", err)
		return
	}

	res.ExpectedSHA256 = expectedChecksum
	res.ChecksumURL = usedURL
	debugf("checksums: %s (sha256=%s)", res.ChecksumURL, res.ExpectedSHA256)
}

// -------- Manifest & Version Fetching --------

type manifest struct {
	Version string            `json:"version"`
	Channel string            `json:"channel"`
	Assets  map[string]string `json:"assets"`
}

// Manifest is exported for testing
type Manifest = manifest

func (m manifest) AssetFor(p platform) (assetURL, checksumURL string) {
	if m.Assets == nil {
		return "", ""
	}
	if v, ok := m.Assets[fmt.Sprintf("%s/%s", p.OS, p.Arch)]; ok {
		assetURL = v
	}
	if v, ok := m.Assets["checksums"]; ok {
		checksumURL = v
	}
	return
}

func fetchLatestManifest(ctx context.Context, force bool) (manifest, error) {
	if !force {
		if cached, ok := readManifestCache(); ok {
			debugf("manifest: using cached latest manifest version=%s", cached.Version)
			return cached, nil
		}
	}

	// Try custom manifest URL override first (for enterprise mirrors).
	if u := strings.TrimSpace(os.Getenv("TNR_LATEST_URL")); u != "" {
		debugf("manifest: trying custom URL %s", u)
		man, err := fetchManifestFromURL(ctx, u)
		if err == nil {
			_ = writeManifestCache(man)
			debugf("manifest: using %s (version=%s)", u, man.Version)
			return man, nil
		}
		debugf("manifest: custom URL failed: %v", err)
	}

	// Default: fetch from GitHub Releases API.
	debugf("manifest: trying GitHub Releases API")
	man, err := fetchLatestFromGitHub(ctx)
	if err != nil {
		return manifest{}, err
	}
	_ = writeManifestCache(man)
	debugf("manifest: using GitHub (version=%s)", man.Version)
	return man, nil
}

func fetchManifestFromURL(ctx context.Context, manifestURL string) (manifest, error) {
	body, err := httpGet(ctx, manifestURL)
	if err != nil {
		return manifest{}, err
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		return manifest{}, err
	}

	var man manifest
	if err := json.Unmarshal(data, &man); err != nil {
		return manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if man.Version == "" {
		return manifest{}, errors.New("manifest missing version")
	}
	return man, nil
}

// fetchLatestFromGitHub calls the GitHub Releases API and converts the response
// into the internal manifest format used for caching and version resolution.
func fetchLatestFromGitHub(ctx context.Context) (manifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubReleasesLatestURL, nil)
	if err != nil {
		return manifest{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient().Do(req)
	if err != nil {
		return manifest{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return manifest{}, fmt.Errorf("github api: http %d (%s)", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return manifest{}, fmt.Errorf("decode github release: %w", err)
	}
	if release.TagName == "" {
		return manifest{}, errors.New("github release missing tag_name")
	}

	assets := make(map[string]string)
	for _, a := range release.Assets {
		key := classifyAsset(a.Name)
		if key != "" {
			assets[key] = a.BrowserDownloadURL
		}
	}

	return manifest{
		Version: release.TagName,
		Channel: "stable",
		Assets:  assets,
	}, nil
}

// classifyAsset maps a release asset filename to a manifest key.
// Returns "" for unrecognized assets (installers, SBOMs, etc.).
func classifyAsset(name string) string {
	lower := strings.ToLower(name)

	if strings.Contains(lower, "checksums") {
		// Prefer the combined checksums file; OS-specific ones are resolved
		// via githubAssetAndChecksum as fallback candidates.
		if !strings.Contains(lower, "-macos") && !strings.Contains(lower, "-linux") && !strings.Contains(lower, "-windows") {
			return "checksums"
		}
		return ""
	}

	// Only match primary archive formats (tar.gz / zip), skip installers (.pkg, .msi, .deb, .rpm).
	isArchive := strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") || strings.HasSuffix(lower, ".zip")
	if !isArchive {
		return ""
	}

	var osKey string
	switch {
	case strings.Contains(lower, "darwin"):
		osKey = "macos"
	case strings.Contains(lower, "linux"):
		osKey = "linux"
	case strings.Contains(lower, "windows"):
		osKey = "windows"
	default:
		return ""
	}

	var archKey string
	switch {
	case strings.Contains(lower, "arm64") || strings.Contains(lower, "aarch64"):
		archKey = "arm64"
	case strings.Contains(lower, "amd64") || strings.Contains(lower, "x86_64"):
		archKey = "amd64"
	default:
		return ""
	}

	return fmt.Sprintf("%s/%s", osKey, archKey)
}

type minVersionPayload struct {
	Version string `json:"version"`
}

func fetchMinVersion(ctx context.Context, force bool) (string, error) {
	if !force {
		if v, ok := readMinVersionCache(); ok {
			debugf("min-version: using cached %s", v)
			return v, nil
		}
	}

	minURL := resolveMinVersionURL()
	if minURL == "" {
		// Opt-in behavior: if no min-version URL is configured, skip mandatory checks.
		debugf("min-version: disabled (no %s)", minVersionEnvKey)
		return "", nil
	}

	body, err := httpGet(ctx, minURL)
	if err != nil {
		return "", err
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	var payload minVersionPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", fmt.Errorf("decode min version: %w", err)
	}
	if payload.Version == "" {
		return "", errors.New("min version payload missing version")
	}
	_ = writeMinVersionCache(payload.Version)
	debugf("min-version: fetched %s from %s", payload.Version, minURL)
	return payload.Version, nil
}

// -------- Checksums --------

func fetchChecksum(ctx context.Context, version, osName string, candidates []string, force bool) (checksum string, usedURL string, err error) {
	if !force {
		if cached, ok := readChecksumCache(version, osName); ok {
			if cached.Unavailable {
				return "", cached.URL, fmt.Errorf("%w for %s (cached)", errChecksumUnavailable, version)
			}
			return cached.Checksum, cached.URL, nil
		}
	}

	var lastErr error
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		sum, ferr := fetchChecksumFromURL(ctx, version, osName, candidate)
		if ferr == nil {
			return sum, candidate, nil
		}
		// A transport or parse failure says more about what went wrong than "that
		// checksum file simply didn't list our archive", so prefer it when reporting.
		if lastErr == nil || (errors.Is(lastErr, errChecksumNotFound) && !errors.Is(ferr, errChecksumNotFound)) {
			lastErr = fmt.Errorf("%s: %w", candidate, ferr)
		}
		if !errors.Is(ferr, errChecksumNotFound) {
			// Try next candidate; remember the one that failed hardest.
			usedURL = candidate
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("%w: no checksum URL candidates for %s", errChecksumUnavailable, version)
	}

	// Remember the failure briefly. Without this every subsequent command retries
	// every candidate, and a release that never publishes checksums for this
	// platform costs the full timeout on each invocation, forever.
	_ = writeChecksumCacheUnavailable(version, osName, usedURL)

	return "", usedURL, lastErr
}

var errChecksumNotFound = errors.New("checksum not found")

// errChecksumUnavailable marks the case where no candidate could supply a usable
// checksum at all, as opposed to a single source not listing the archive.
var errChecksumUnavailable = errors.New("no verifiable checksum available")

func fetchChecksumFromURL(ctx context.Context, version, osName, checksumURL string) (string, error) {
	body, err := httpGet(ctx, checksumURL)
	if err != nil {
		return "", err
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}

	targetFile := targetArchiveName(version, osName)
	checksum, err := parseChecksum(string(data), targetFile)
	if err != nil {
		if errors.Is(err, errChecksumNotFound) {
			return "", err
		}
		return "", fmt.Errorf("parse checksum: %w", err)
	}
	_ = writeChecksumCache(version, osName, checksum, checksumURL)
	return checksum, nil
}

func parseChecksum(data, target string) (string, error) {
	target = strings.TrimSpace(target)
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		name := parts[len(parts)-1]
		if name == target || strings.HasSuffix(name, "/"+target) || strings.HasSuffix(name, "\\"+target) {
			checksum := strings.TrimSpace(parts[0])
			if len(checksum) != sha256HexLength {
				return "", fmt.Errorf("invalid SHA-256 checksum for %s", target)
			}
			if _, err := hex.DecodeString(checksum); err != nil {
				return "", fmt.Errorf("invalid SHA-256 checksum for %s: %w", target, err)
			}
			return strings.ToLower(checksum), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%w: %s", errChecksumNotFound, target)
}

const sha256HexLength = 64

// -------- Helpers --------

type platform struct {
	OS   string
	Arch string
	Ext  string
}

// Platform is exported for testing
type Platform = platform

// detectPlatform detects the current platform
func detectPlatform() platform {
	var p platform
	switch runtime.GOOS {
	case "darwin":
		p.OS = "macos"
		p.Ext = ".tar.gz"
	case "linux":
		p.OS = "linux"
		p.Ext = ".tar.gz"
	case "windows":
		p.OS = "windows"
		p.Ext = ".zip"
	default:
		p.OS = runtime.GOOS
		p.Ext = ".tar.gz"
	}

	switch runtime.GOARCH {
	case "amd64":
		p.Arch = "amd64"
	case "arm64":
		p.Arch = "arm64"
	default:
		p.Arch = runtime.GOARCH
	}
	return p
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	return v
}

// targetArchiveName generates the expected archive filename for checksum lookup.
// It must match the naming used in release artifacts, e.g. tnr_2.0.1_linux_amd64.tar.gz.
func targetArchiveName(version, osName string) string {
	v := normalizeVersion(version)

	fileOS := osName
	if osName == "macos" {
		fileOS = "darwin"
	}

	return fmt.Sprintf("tnr_%s_%s_%s%s", v, fileOS, detectPlatform().Arch, detectPlatform().Ext)
}

func checksumCandidates(explicitURL, assetURL, version string, osName string) []string {
	candidates := make([]string, 0, 4)
	if explicitURL != "" {
		candidates = append(candidates, explicitURL)
	}
	releaseRoot := deriveReleaseRoot(assetURL)
	if releaseRoot != "" {
		candidates = append(candidates,
			fmt.Sprintf("%s/checksums-%s.txt", releaseRoot, osName),
			fmt.Sprintf("%s/checksums.txt", releaseRoot),
			fmt.Sprintf("%s/%s/checksums-%s.txt", releaseRoot, osName, osName),
		)
	}
	return candidates
}

func deriveReleaseRoot(assetURL string) string {
	if assetURL == "" {
		return ""
	}
	u, err := url.Parse(assetURL)
	if err != nil {
		return ""
	}
	u.Path = path.Dir(u.Path) // .../{tag}
	return strings.TrimSuffix(u.String(), "/")
}

// githubAssetAndChecksum builds GitHub release URLs for the asset and OS-specific checksum file.
// Tag is v{version}, filenames use plain {version}.
func githubAssetAndChecksum(version string, p platform) (assetURL, checksumURL string) {
	fileVersion := normalizeVersion(version)
	tag := fileVersion
	if !strings.HasPrefix(tag, "v") && !strings.HasPrefix(tag, "V") {
		tag = "v" + tag
	}
	fileOS := p.OS
	if p.OS == "macos" {
		fileOS = "darwin"
	}
	assetURL = fmt.Sprintf(
		"%s/%s/tnr_%s_%s_%s%s",
		strings.TrimSuffix(githubReleaseDownloadBaseURL, "/"), tag, fileVersion, fileOS, p.Arch, p.Ext,
	)
	checkName := "checksums.txt"
	switch p.OS {
	case "macos":
		checkName = "checksums-macos.txt"
	case "linux":
		checkName = "checksums-linux.txt"
	case "windows":
		checkName = "checksums-windows.txt"
	}
	checksumURL = fmt.Sprintf(
		"%s/%s/%s",
		strings.TrimSuffix(githubReleaseDownloadBaseURL, "/"), tag, checkName,
	)
	return
}

func resolveMinVersionURL() string {
	if v := strings.TrimSpace(os.Getenv(minVersionEnvKey)); v != "" {
		return v
	}
	return ""
}

// -------- Cache helpers --------

type manifestCache struct {
	CheckedAt time.Time `json:"checked_at"`
	Manifest  manifest  `json:"manifest"`
}

type minVersionCache struct {
	CheckedAt time.Time `json:"checked_at"`
	Version   string    `json:"version"`
}

type checksumCache struct {
	CheckedAt time.Time `json:"checked_at"`
	Checksum  string    `json:"checksum"`
	URL       string    `json:"url"`
	// Unavailable records that no candidate produced a usable checksum. Cached for
	// negativeChecksumCacheTTL only, so a republished release is picked up soon.
	Unavailable bool `json:"unavailable,omitempty"`
}

func readManifestCache() (manifest, bool) {
	var cached manifestCache
	if !readJSONCache(manifestCacheName, &cached) {
		return manifest{}, false
	}
	if time.Since(cached.CheckedAt) > cacheTTL {
		return manifest{}, false
	}
	return cached.Manifest, true
}

func writeManifestCache(man manifest) error {
	return writeJSONCache(manifestCacheName, manifestCache{
		CheckedAt: time.Now(),
		Manifest:  man,
	})
}

func readMinVersionCache() (string, bool) {
	var cached minVersionCache
	if !readJSONCache(minVersionCacheName, &cached) {
		return "", false
	}
	if time.Since(cached.CheckedAt) > cacheTTL {
		return "", false
	}
	return cached.Version, true
}

func writeMinVersionCache(version string) error {
	return writeJSONCache(minVersionCacheName, minVersionCache{
		CheckedAt: time.Now(),
		Version:   version,
	})
}

func readChecksumCache(version, osName string) (checksumCache, bool) {
	name := fmt.Sprintf(checksumsCacheTemplate, version, osName)
	var cached checksumCache
	if !readJSONCache(name, &cached) {
		return checksumCache{}, false
	}
	ttl := cacheTTL
	if cached.Unavailable {
		ttl = negativeChecksumCacheTTL
	}
	if time.Since(cached.CheckedAt) > ttl {
		return checksumCache{}, false
	}
	return cached, true
}

func writeChecksumCache(version, osName, checksum, url string) error {
	name := fmt.Sprintf(checksumsCacheTemplate, version, osName)
	return writeJSONCache(name, checksumCache{
		CheckedAt: time.Now(),
		Checksum:  checksum,
		URL:       url,
	})
}

func writeChecksumCacheUnavailable(version, osName, url string) error {
	name := fmt.Sprintf(checksumsCacheTemplate, version, osName)
	return writeJSONCache(name, checksumCache{
		CheckedAt:   time.Now(),
		URL:         url,
		Unavailable: true,
	})
}

func ClearChecksumCache(version string) error {
	version = normalizeVersion(version)
	for _, candidate := range []string{version, "v" + version, "V" + version} {
		name := fmt.Sprintf(checksumsCacheTemplate, candidate, detectPlatform().OS)
		path, err := cachePath(name)
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func cachePath(name string) (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			if err != nil {
				return "", err
			}
			return "", homeErr
		}
		dir = filepath.Join(home, ".cache")
	}
	dir = filepath.Join(dir, cacheDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func readJSONCache(name string, dst any) bool {
	path, err := cachePath(name)
	if err != nil {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return false
	}
	return true
}

func writeJSONCache(name string, payload any) error {
	path, err := cachePath(name)
	if err != nil {
		return err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// -------- Networking --------

func httpClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
	}
}

func httpGet(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("http %d: %s (%s)", resp.StatusCode, resp.Status, strings.TrimSpace(string(body)))
	}
	return resp.Body, nil
}
