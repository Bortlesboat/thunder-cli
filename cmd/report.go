package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Thunder-Compute/thunder-cli/api"
	"github.com/Thunder-Compute/thunder-cli/internal/version"
	"github.com/Thunder-Compute/thunder-cli/tui"
	helpmenus "github.com/Thunder-Compute/thunder-cli/tui/help-menus"
	termx "github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

const maxDetailsFileBytes = utf8.UTFMax * api.MaxReportDetailsRunes

var reportCmd = newReportCommand()

func newReportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "report",
		Short:        "Report a bug or share product feedback",
		SilenceUsage: true,
		Long:         "Send a bug report or product feedback to Thunder support. In a terminal, tnr report asks for the report type, a summary and details, then shows the report for review before sending. Flags fill in their steps. Only your supplied text and the displayed context are included.\nUse --details-file - to read standard input. Dry run requires no login.",
		Args:         wrapArgs(cobra.NoArgs),
		Annotations:  map[string]string{"skipUpdateCheck": "true"},
		RunE:         runReport,
	}
	cmd.Flags().String("subject", "", fmt.Sprintf("Short summary (1–%d Unicode characters)", api.MaxReportSubjectRunes))
	cmd.Flags().String("details", "", fmt.Sprintf("Report text (1–%d Unicode characters)", api.MaxReportDetailsRunes))
	cmd.Flags().String("details-file", "", "Read report text from this file, or - for standard input")
	cmd.Flags().String("kind", "bug", "Report kind: "+strings.Join(api.ReportKinds, " or "))
	cmd.Flags().String("component", "", "Suspected component: "+strings.Join(api.ReportComponents, ", "))
	cmd.Flags().String("instance", "", "Instance identifier (mutually exclusive with --sandbox)")
	cmd.Flags().String("sandbox", "", "Sandbox identifier (mutually exclusive with --instance)")
	cmd.Flags().String("occurred-at", "", "Occurrence time in RFC 3339 format")
	cmd.Flags().Bool("dry-run", false, "Preview the report without submitting or authenticating")
	cmd.SetHelpFunc(wrapHelp(helpmenus.RenderReportHelp))
	return cmd
}

func init() { rootCmd.AddCommand(reportCmd) }

func runReport(cmd *cobra.Command, _ []string) error {
	if cmd.Flags().Changed("details") && cmd.Flags().Changed("details-file") {
		return usageErr("--details and --details-file are mutually exclusive")
	}
	if cmd.Flags().Changed("instance") && cmd.Flags().Changed("sandbox") {
		return usageErr("--instance and --sandbox are mutually exclusive")
	}
	input := reportRequestFromFlags(cmd)
	if cmd.Flags().Changed("details-file") {
		details, err := readDetailsFile(cmd)
		if err != nil {
			return err
		}
		input.Details = details
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	interactive := tui.IsInteractive() && termx.IsTerminal(os.Stdin.Fd())
	if err := validateReportInput(&input, interactive); err != nil {
		return err
	}
	var client *api.Client
	if !dryRun {
		var err error
		client, err = getAuthenticatedClient()
		if err != nil {
			return err
		}
	}
	confirm := interactive && !dryRun && !YesFlag
	if interactive && (input.Subject == "" || input.Details == "" || confirm) {
		preset := tui.ReportInput{Subject: input.Subject, Details: input.Details}
		if cmd.Flags().Changed("kind") {
			preset.Kind = input.ReportKind
		}
		report, err := tui.RunReport(preset, []tui.ReportField{
			{Label: "Component", Value: input.Context.SuspectedComponent},
			{Label: "Instance", Value: input.Context.InstanceID},
			{Label: "Sandbox", Value: input.Context.SandboxID},
			{Label: "Occurred at", Value: input.Context.OccurredAt},
		}, confirm)
		if err != nil {
			return reportCancellation(err)
		}
		input.ReportKind, input.Subject, input.Details = report.Kind, report.Subject, report.Details
	}
	if err := validateReportInput(&input, false); err != nil {
		return err
	}
	preview := reportPreview{
		DryRun:    dryRun,
		Requester: "Resolved by the server after authentication; absent from this preview.",
		Report:    input,
	}
	if dryRun {
		printJSON(preview)
		return nil
	}
	if !YesFlag && !interactive {
		return usageErr("provide --subject and --details or --details-file, and use --yes to submit noninteractively; use --dry-run to preview")
	}
	if interactive && YesFlag {
		printJSON(preview)
	}
	result, err := client.SubmitReport(cmd.Context(), input)
	if err != nil {
		return &reportSubmissionError{Err: err, Report: input}
	}
	if JSONOutput {
		printJSON(result)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), tui.RenderSuccessSimple("Report submitted successfully!"))
		fmt.Fprintf(cmd.OutOrStdout(), "Reference: %s\n", result.ReportID)
	}
	return nil
}

func reportRequestFromFlags(cmd *cobra.Command) api.ReportRequest {
	get := func(name string) string {
		value, _ := cmd.Flags().GetString(name)
		return value
	}
	return api.ReportRequest{
		Subject:    get("subject"),
		Details:    get("details"),
		ReportKind: get("kind"),
		Source:     "cli",
		Context: api.ReportContext{
			SuspectedComponent: get("component"),
			OccurredAt:         get("occurred-at"),
			InstanceID:         get("instance"),
			SandboxID:          get("sandbox"),
			PreparedAt:         time.Now().UTC().Format(time.RFC3339Nano),
			CLIVersion:         version.BuildVersion,
			BuildCommit:        version.BuildCommit,
			BuildDate:          version.BuildDate,
			OS:                 runtime.GOOS,
			Architecture:       runtime.GOARCH,
		},
	}
}

func readDetailsFile(cmd *cobra.Command) (string, error) {
	path, _ := cmd.Flags().GetString("details-file")
	var reader io.Reader = cmd.InOrStdin()
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return "", usageErr("could not open --details-file: %v", err)
		}
		defer file.Close()
		reader = file
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxDetailsFileBytes+1))
	if err != nil {
		return "", usageErr("could not read --details-file: %v", err)
	}
	if len(data) > maxDetailsFileBytes {
		return "", usageErr("details file exceeds %d bytes; details must contain at most %d Unicode characters", maxDetailsFileBytes, api.MaxReportDetailsRunes)
	}
	return string(data), nil
}

func reportCancellation(err error) error {
	if errors.Is(err, tui.ErrCancelled) {
		return context.Canceled
	}
	return err
}

type reportPreview struct {
	DryRun    bool              `json:"dry_run"`
	Submitted bool              `json:"submitted"`
	Requester string            `json:"requester"`
	Report    api.ReportRequest `json:"report"`
}

type reportSubmissionError struct {
	Err    error
	Report api.ReportRequest
}

func (e *reportSubmissionError) Error() string {
	body, _ := json.MarshalIndent(e.Report, "", "  ")
	return strings.TrimSuffix(e.Err.Error(), ".") + ". Your report input is retained below:\n" + string(body)
}
func (e *reportSubmissionError) Unwrap() error    { return e.Err }
func (e *reportSubmissionError) UserFacing() bool { return true }
func (e *reportSubmissionError) JSONErrorOutput() any {
	code, message := "support_submission_failed", e.Err.Error()
	var apiErr *api.APIError
	if errors.As(e.Err, &apiErr) {
		code, message = apiErr.Code, apiErr.Message
	}
	return struct {
		OK      bool              `json:"ok"`
		Error   string            `json:"error"`
		Message string            `json:"message"`
		Report  api.ReportRequest `json:"report"`
	}{false, code, message, e.Report}
}

func validateReportInput(input *api.ReportRequest, allowMissing bool) error {
	input.Subject, input.Details = strings.TrimSpace(input.Subject), strings.TrimSpace(input.Details)
	if err := validateReportText(input.Subject, "--subject", api.MaxReportSubjectRunes, allowMissing); err != nil {
		return err
	}
	if err := validateReportText(input.Details, "--details or --details-file", api.MaxReportDetailsRunes, allowMissing); err != nil {
		return err
	}
	category, ok := api.ReportCategories[input.ReportKind]
	if !ok {
		return usageErr("--kind must be %s", strings.Join(api.ReportKinds, " or "))
	}
	input.Category = category
	if component := input.Context.SuspectedComponent; component != "" && !slices.Contains(api.ReportComponents, component) {
		return usageErr("--component must be one of: %s", strings.Join(api.ReportComponents, ", "))
	}
	for _, field := range []*string{&input.Context.InstanceID, &input.Context.SandboxID} {
		*field = strings.TrimSpace(*field)
		if !utf8.ValidString(*field) || utf8.RuneCountInString(*field) > api.MaxReportResourceIDRunes || strings.ContainsAny(*field, "\x00\r\n") {
			return usageErr("resource identifiers must contain at most %d characters without line breaks", api.MaxReportResourceIDRunes)
		}
	}
	if input.Context.OccurredAt != "" {
		timestamp, err := time.Parse(time.RFC3339, input.Context.OccurredAt)
		if err != nil {
			return usageErr("--occurred-at must be an RFC 3339 timestamp")
		}
		input.Context.OccurredAt = timestamp.UTC().Format(time.RFC3339Nano)
	}
	body, _ := json.Marshal(input)
	if len(body) > api.MaxReportRequestBytes {
		return usageErr("encoded report exceeds the %d KiB request limit", api.MaxReportRequestBytes/1024)
	}
	return nil
}

func validateReportText(text, flags string, limit int, allowMissing bool) error {
	if text == "" && !allowMissing {
		return usageErr("%s is required", flags)
	}
	if !utf8.ValidString(text) {
		return usageErr("%s must be valid UTF-8 text", flags)
	}
	if utf8.RuneCountInString(text) > limit {
		return usageErr("%s must be at most %d Unicode characters", flags, limit)
	}
	return nil
}
