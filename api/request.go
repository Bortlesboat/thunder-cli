package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Thunder-Compute/thunder-cli/internal/version"
	"github.com/getsentry/sentry-go"
)

func (c *Client) doRequest(ctx context.Context, method, path string, body, result interface{}) error {
	addRequestBreadcrumb(method, path)

	var bodyData []byte
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
		bodyData = data
	}
	return c.withAuthentication(ctx, func(token string) error {
		return c.doRequestWithToken(ctx, method, path, bodyData, result, token)
	})
}

func (c *Client) withAuthentication(ctx context.Context, attempt func(string) error) error {
	token, err := c.tokenSource.Token(ctx)
	if err != nil {
		return fmt.Errorf("failed to load authentication token: %w", err)
	}

	err = attempt(token)
	var apiErr *APIError
	refreshing, canRefresh := c.tokenSource.(RefreshingTokenSource)
	if !canRefresh || !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		return err
	}

	refreshedToken, err := refreshing.Refresh(ctx, token)
	if err != nil {
		return err
	}
	return attempt(refreshedToken)
}

func (c *Client) doRequestWithToken(ctx context.Context, method, path string, bodyData []byte, result interface{}, token string) error {
	resp, err := c.sendRequestWithToken(ctx, method, path, bodyData, token, c.httpClient)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	captureServerError(method, path, resp.StatusCode)
	return c.decodeAPIResponse(resp, result)
}

func (c *Client) sendRequestWithToken(ctx context.Context, method, path string, bodyData []byte, token string, httpClient *http.Client) (*http.Response, error) {
	var bodyReader io.Reader
	if bodyData != nil {
		bodyReader = bytes.NewReader(bodyData)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Thunder-Client", "GO-CLI")
	req.Header.Set("Version", version.BuildVersion)
	if platform := cliPlatform(); platform != "" {
		req.Header.Set("Platform", platform)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to make request: %w", ErrTransport, err)
	}
	return resp, nil
}

func (c *Client) decodeAPIResponse(resp *http.Response, result any) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return c.apiHTTPError(resp, body)
	}
	if result != nil && len(body) > 0 {
		if err := json.Unmarshal(body, result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}
	}
	return nil
}

func (c *Client) apiHTTPError(resp *http.Response, body []byte) *APIError {
	if resp.StatusCode == http.StatusUnauthorized {
		return &APIError{StatusCode: resp.StatusCode, Code: "authorization_failed", Message: c.authenticationErrorMessage}
	}
	retryAfter, hasRetryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
	result := &APIError{StatusCode: resp.StatusCode, RetryAfter: retryAfter, HasRetryAfter: hasRetryAfter}
	var parsed struct {
		Message string `json:"message"`
		Code    string `json:"error"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.Message != "" {
		result.Message, result.Code = parsed.Message, parsed.Code
	} else {
		result.Message = fmt.Sprintf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}
	return result
}

func addRequestBreadcrumb(method, path string) {
	sentry.AddBreadcrumb(&sentry.Breadcrumb{Category: "api", Message: method + " " + path, Level: sentry.LevelInfo})
}

func captureServerError(method, path string, status int) {
	if status < http.StatusInternalServerError {
		return
	}
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("api_method", method)
		scope.SetTag("api_path", path)
		scope.SetTag("status_code", fmt.Sprintf("%d", status))
		scope.SetLevel(sentry.LevelError)
		sentry.CaptureMessage(fmt.Sprintf("API server error: %s %s returned %d", method, path, status))
	})
}
