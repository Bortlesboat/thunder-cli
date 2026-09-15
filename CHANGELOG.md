# Changelog

All notable Thunder CLI changes are recorded here. Releases before this
change-fragment process was adopted remain available on the
[GitHub releases page](https://github.com/Thunder-Compute/thunder-cli/releases).


## v2.0.72 - 2026-09-15

### Added
- Sign in from SSH and headless machines with automatic manual-login instructions and --manual. Paste a token or callback URL, and retry errors without restarting login.

### Fixed
- Keep shared command progress and monitored status stable across terminal resizes without losing terminal scrollback.
- Make status and snapshot listing with --no-wait print once and exit in interactive terminals.
- Show GPU count selection again when reselecting a GPU type in interactive tnr create, while honoring valid command-line presets.
- Keep commands running when an automatic update fails.
- Verify release archive checksums before publishing and validate complete release artifacts during dry runs.
- Make status --no-wait display the same styled table and last-updated timestamp as monitoring, then exit immediately.
- Retry failed updates after one hour per version, refresh stale checksums, and allow immediate retries with tnr update.
- Preserve macOS and Linux checksums when rebuilding Windows assets, and stop publishing if the existing release cannot be verified.

<!-- Internal changes -->
<!-- Align CLI documentation with Thunder Compute's low-cost GPU positioning for developers. -->
<!-- Consolidate CLI release-note rules into cli/AGENTS.md; the root AGENTS.md now points here instead of duplicating them. -->
<!-- Add a reproducible local self-update check using the published CLI, disposable binaries, and simulated release failures. -->

**Full Changelog**: https://github.com/Thunder-Compute/thunder-cli/compare/v2.0.71...v2.0.72

## v2.0.71 - 2026-07-30

### Fixed
- Keep CLI text legible across light, dark, and limited-color terminal themes.
- Keep browser authentication callback pages readable in light themes.
- Allow create presets to use GPU types provided by the current GPU configuration and show unavailable snapshots.
- Keep installer completion screens readable when the system uses a dark theme.
- Preserve the login link and stable layout when the terminal is resized.
- Keep JSON command output machine-readable and free from interactive text.
- Prevent similarly prefixed instance IDs from colliding in SSH configuration.

### Security
- Harden browser login by validating the OAuth callback state.

<!-- Internal changes -->
<!-- Adds release governance and automation without changing shipped CLI behavior. -->

**Full Changelog**: https://github.com/Thunder-Compute/thunder-cli/compare/v2.0.70...v2.0.71

<!-- Changie baseline: releases through v2.0.70 predate this changelog. -->
