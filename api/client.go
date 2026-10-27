package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Thunder-Compute/thunder-cli/internal/clierr"
	"github.com/Thunder-Compute/thunder-cli/pkg/types"
	"github.com/getsentry/sentry-go"
	sentryhttpclient "github.com/getsentry/sentry-go/httpclient"
)

// ErrTransport marks transport-level HTTP failures where http.Client.Do could
// not complete the request — DNS failure, connection refused, connection
// reset, TLS handshake error, mid-stream EOF, etc. Callers use errors.Is to
// classify these uniformly as user/network noise, distinct from *APIError
// (which means the server did respond with a status >= 400).
var ErrTransport = clierr.New("transport error")

// APIError is returned for HTTP responses with status >= 400.
type APIError struct {
	StatusCode    int
	Code          string
	Message       string
	RetryAfter    time.Duration
	HasRetryAfter bool
}

func (e *APIError) Error() string {
	return e.Message
}

func (e *APIError) UserFacing() bool {
	return true
}

type Client struct {
	baseURL                    string
	tokenSource                TokenSource
	httpClient                 *http.Client
	authenticationErrorMessage string
}

// TokenSource supplies an access token immediately before each API request.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// RefreshingTokenSource can rotate a credential after an authenticated API
// request rejects the exact token that was sent.
type RefreshingTokenSource interface {
	TokenSource
	Refresh(ctx context.Context, rejectedToken string) (string, error)
}

type staticTokenSource string

func (s staticTokenSource) Token(context.Context) (string, error) { return string(s), nil }

func NewClient(token, baseURL string) *Client {
	return NewClientWithTokenSource(staticTokenSource(token), baseURL)
}

func NewClientWithTokenSource(tokenSource TokenSource, baseURL string) *Client {
	sentry.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetTag("api_url", baseURL)
	})
	return &Client{
		baseURL:                    baseURL,
		tokenSource:                tokenSource,
		authenticationErrorMessage: "authentication failed: invalid token",
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: sentryhttpclient.NewSentryRoundTripper(nil),
		},
	}
}

func (c *Client) SetAuthenticationErrorMessage(message string) {
	c.authenticationErrorMessage = message
}

func cliPlatform() string {
	switch runtime.GOOS {
	case "darwin":
		return "mac"
	case "linux", "windows":
		return runtime.GOOS
	default:
		return ""
	}
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}

	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second, true
	}

	retryAt, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	if retryAt.Before(now) {
		return 0, true
	}
	return retryAt.Sub(now), true
}

// sortedInstances converts a map keyed by ID into a sorted slice.
func sortedInstances(raw map[string]Instance) []Instance {
	instances := make([]Instance, 0, len(raw))
	for id, inst := range raw {
		inst.ID = id
		instances = append(instances, inst)
	}
	sort.Slice(instances, func(i, j int) bool {
		return instances[i].ID < instances[j].ID
	})
	return instances
}

func (c *Client) ValidateToken(ctx context.Context) (*ValidateTokenResult, error) {
	var result ValidateTokenResult
	if err := c.doRequest(ctx, "GET", "/v1/auth/validate", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) RevokeCLIOAuthGrant(ctx context.Context) error {
	return c.doRequest(ctx, http.MethodDelete, "/v1/auth/cli/grant", nil, nil)
}

// GetSubscriptionStatusCtx returns the caller's Stripe subscription status
// (e.g. "active", "past_due", "canceled") from /v1/user-data.
func (c *Client) GetSubscriptionStatusCtx(ctx context.Context) (string, error) {
	var result struct {
		Data struct {
			SubscriptionStatus string `json:"subscriptionStatus"`
		} `json:"data"`
	}
	if err := c.doRequest(ctx, "GET", "/v1/user-data", nil, &result); err != nil {
		return "", err
	}
	return result.Data.SubscriptionStatus, nil
}

func (c *Client) ListInstancesWithIPUpdateCtx(ctx context.Context) ([]Instance, error) {
	var raw map[string]Instance
	if err := c.doRequest(ctx, "GET", "/v1/instances/list?update_ips=true", nil, &raw); err != nil {
		return nil, err
	}
	return sortedInstances(raw), nil
}

func (c *Client) ListInstancesWithIPUpdate() ([]Instance, error) {
	return c.ListInstancesWithIPUpdateCtx(context.Background())
}

func (c *Client) ListInstances() ([]Instance, error) {
	var raw map[string]Instance
	if err := c.doRequest(context.Background(), "GET", "/v1/instances/list", nil, &raw); err != nil {
		return nil, err
	}
	return sortedInstances(raw), nil
}

func (c *Client) AddSSHKeyCtx(ctx context.Context, instanceID string) (*AddSSHKeyResponse, error) {
	var resp AddSSHKeyResponse
	if err := c.doRequest(ctx, "POST", fmt.Sprintf("/v1/instances/%s/add_key", instanceID), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) AddSSHKey(instanceID string) (*AddSSHKeyResponse, error) {
	return c.AddSSHKeyCtx(context.Background(), instanceID)
}

func (c *Client) ListTemplates() ([]TemplateEntry, error) {
	var raw types.ThunderTemplatesResponse
	if err := c.doRequest(context.Background(), "GET", "/v1/thunder-templates", nil, &raw); err != nil {
		return nil, err
	}
	entries := make([]TemplateEntry, 0, len(raw))
	for key, tmpl := range raw {
		entries = append(entries, TemplateEntry{Key: key, Template: tmpl})
	}
	return entries, nil
}

func (c *Client) CreateInstance(req CreateInstanceRequest) (*CreateInstanceResponse, error) {
	var resp CreateInstanceResponse
	if err := c.doRequest(context.Background(), "POST", "/v1/instances/create", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DeleteInstance(instanceID string) (*DeleteInstanceResponse, error) {
	if err := c.doRequest(context.Background(), "POST", fmt.Sprintf("/v1/instances/%s/delete", instanceID), nil, nil); err != nil {
		return nil, err
	}
	return &DeleteInstanceResponse{Message: "Instance deleted successfully", Success: true}, nil
}

// ModifyInstance modifies an existing instance configuration.
func (c *Client) ModifyInstance(instanceID string, req InstanceModifyRequest) (*InstanceModifyResponse, error) {
	var resp InstanceModifyResponse
	err := c.doRequest(context.Background(), "POST", fmt.Sprintf("/v1/instances/%s/modify", instanceID), req, &resp)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			switch apiErr.StatusCode {
			case 404:
				return nil, fmt.Errorf("instance not found")
			case 409:
				return nil, fmt.Errorf("instance cannot be modified (may not be in RUNNING state)")
			}
		}
		return nil, err
	}
	return &resp, nil
}

// UpdatePorts atomically adds and removes forwarded HTTP ports on an instance.
func (c *Client) UpdatePorts(instanceID string, addPorts, removePorts []int) (*PortAddResponse, error) {
	if len(addPorts) == 0 && len(removePorts) == 0 {
		return nil, fmt.Errorf("no ports specified")
	}
	var resp PortAddResponse
	err := c.doRequest(context.Background(), "PATCH", fmt.Sprintf("/v1/instances/%s/ports", instanceID), PortUpdateRequest{
		AddPorts:    addPorts,
		RemovePorts: removePorts,
	}, &resp)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
			return nil, fmt.Errorf("instance not found")
		}
		return nil, err
	}
	return &resp, nil
}

// RemovePorts removes one or more forwarded HTTP ports from an instance in a single call.
func (c *Client) RemovePorts(instanceID string, ports []int) error {
	if len(ports) == 0 {
		return nil
	}
	_, err := c.UpdatePorts(instanceID, nil, ports)
	return err
}

func (c *Client) CreateSnapshot(req CreateSnapshotRequest) (*CreateSnapshotResponse, error) {
	var resp CreateSnapshotResponse
	if err := c.doRequest(context.Background(), "POST", "/v1/snapshots/create", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListSnapshots() (ListSnapshotsResponse, error) {
	var resp ListSnapshotsResponse
	if err := c.doRequest(context.Background(), "GET", "/v1/snapshots/list", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) DeleteSnapshot(snapshotID string) error {
	return c.doRequest(context.Background(), "DELETE", fmt.Sprintf("/v1/snapshots/%s", snapshotID), nil, nil)
}

// FetchPricing retrieves the public pricing data from the API.
func (c *Client) FetchPricing() (map[string]float64, error) {
	var result struct {
		Pricing map[string]float64 `json:"pricing"`
	}
	if err := c.doRequest(context.Background(), "GET", "/v2/pricing", nil, &result); err != nil {
		return nil, err
	}
	return result.Pricing, nil
}

// GetSpecs retrieves GPU spec configurations from the API.
func (c *Client) GetSpecs() (map[string]GpuSpecConfig, error) {
	var result struct {
		Specs map[string]GpuSpecConfig `json:"specs"`
	}
	if err := c.doRequest(context.Background(), "GET", "/v2/specs", nil, &result); err != nil {
		return nil, err
	}
	return result.Specs, nil
}

// GetAvailability retrieves per-spec GPU availability from the API.
func (c *Client) GetAvailability() (*GPUAvailabilityResponse, error) {
	var result GPUAvailabilityResponse
	if err := c.doRequest(context.Background(), "GET", "/v2/status", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
