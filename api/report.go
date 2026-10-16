package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
)

const (
	supportReportPath      = "/support/tickets"
	maxReportResponseBytes = 64 << 10
)

// The API's support ticket limits and accepted values, which reports must meet.
const (
	MaxReportSubjectRunes    = 200
	MaxReportDetailsRunes    = 10000
	MaxReportResourceIDRunes = 128
	MaxReportRequestBytes    = 64 << 10
)

var (
	// ReportCategories maps each report kind to the support category the API
	// requires with it.
	ReportCategories = map[string]string{"bug": "technical_issue", "feedback": "product_feedback"}
	ReportKinds      = slices.Sorted(maps.Keys(ReportCategories))
	ReportComponents = []string{"cli", "console", "api", "mcp", "virtualization", "unknown"}
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
	resp, err := c.sendRequestWithToken(ctx, http.MethodPost, supportReportPath, body, token)
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
		apiErr := c.apiHTTPError(resp, body)
		if apiErr.Code == "" {
			apiErr.Code, apiErr.Message = reportErrorFallback(resp.StatusCode)
		}
		return nil, apiErr
	}
	var result ReportResponse
	if err != nil || len(body) > maxReportResponseBytes || resp.StatusCode >= http.StatusMultipleChoices ||
		json.Unmarshal(body, &result) != nil || !result.OK || result.ReportID == "" {
		return nil, fmt.Errorf("unexpected response from Thunder support (HTTP %d)", resp.StatusCode)
	}
	return &result, nil
}

func reportErrorFallback(status int) (code, message string) {
	switch status {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return "invalid_support_ticket", "The report was rejected. Check the input and its size limits."
	case http.StatusForbidden:
		return "authorization_failed", "You are not authorized to submit this report."
	case http.StatusTooManyRequests:
		return "support_rate_limited", "Support reporting is temporarily rate limited."
	}
	return "support_submission_failed", "The report was not accepted."
}
