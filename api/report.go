package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	supportReportPath      = "/support/tickets"
	maxReportResponseBytes = 64 << 10
)

type ReportContext struct {
	PreparedAt         string `json:"prepared_at,omitempty"`
	SuspectedComponent string `json:"suspected_component,omitempty"`
	OccurredAt         string `json:"occurred_at,omitempty"`
	InstanceID         string `json:"instance_id,omitempty"`
	SandboxID          string `json:"sandbox_id,omitempty"`
	CLIVersion         string `json:"cli_version,omitempty"`
	BuildCommit        string `json:"build_commit,omitempty"`
	BuildDate          string `json:"build_date,omitempty"`
	OS                 string `json:"os,omitempty"`
	Architecture       string `json:"architecture,omitempty"`
}

type ReportRequest struct {
	Subject    string        `json:"subject"`
	Category   string        `json:"category"`
	Details    string        `json:"details"`
	ReportKind string        `json:"report_kind"`
	Source     string        `json:"source"`
	Context    ReportContext `json:"context"`
}

type ReportResponse struct {
	OK       bool   `json:"ok"`
	ReportID string `json:"report_id"`
}

func (c *Client) SubmitReport(ctx context.Context, input ReportRequest) (*ReportResponse, error) {
	addRequestBreadcrumb(http.MethodPost, supportReportPath)
	body, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	var result *ReportResponse
	err = c.withAuthentication(ctx, func(token string) error {
		var attemptErr error
		result, attemptErr = c.submitReportWithToken(ctx, body, token)
		return attemptErr
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) submitReportWithToken(ctx context.Context, body []byte, token string) (*ReportResponse, error) {
	httpClient := *c.httpClient
	httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.sendRequestWithToken(ctx, http.MethodPost, supportReportPath, body, token, &httpClient)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	captureServerError(http.MethodPost, supportReportPath, resp.StatusCode)
	return c.decodeReportResponse(resp)
}

func (c *Client) decodeReportResponse(resp *http.Response) (*ReportResponse, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxReportResponseBytes+1))
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, reportHTTPError(resp.StatusCode, body, resp.Header.Get("Retry-After"), c.authenticationErrorMessage)
	}
	var result ReportResponse
	if err != nil || len(body) > maxReportResponseBytes || resp.StatusCode >= http.StatusMultipleChoices ||
		json.Unmarshal(body, &result) != nil || !result.OK || result.ReportID == "" {
		return nil, fmt.Errorf("unexpected response from Thunder support (HTTP %d)", resp.StatusCode)
	}
	return &result, nil
}

func reportHTTPError(status int, body []byte, retryAfter, authenticationMessage string) *APIError {
	result := &APIError{StatusCode: status}
	result.RetryAfter, result.HasRetryAfter = parseRetryAfter(retryAfter, time.Now())
	if status == http.StatusUnauthorized {
		result.Code, result.Message = "authorization_failed", authenticationMessage
		return result
	}
	var parsed struct {
		Code    string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.Code != "" && parsed.Message != "" {
		result.Code, result.Message = parsed.Code, parsed.Message
		return result
	}
	switch status {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		result.Code, result.Message = "invalid_support_ticket", "The report was rejected. Check the input and its size limits."
	case http.StatusForbidden:
		result.Code, result.Message = "authorization_failed", "You are not authorized to submit this report."
	case http.StatusTooManyRequests:
		result.Code, result.Message = "support_rate_limited", "Support reporting is temporarily rate limited."
	default:
		result.Code, result.Message = "support_submission_failed", "The report was not accepted."
	}
	return result
}
