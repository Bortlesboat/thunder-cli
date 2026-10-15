package helpmenus

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func RenderReportHelp(cmd *cobra.Command) {
	InitHelpStyles(os.Stdout)

	var output strings.Builder

	header := HelpHeader("REPORT COMMAND", "Report a bug or share product feedback")

	output.WriteString(HeaderStyle.Render(header))

	// Usage Section
	output.WriteString(SectionStyle.Render("● USAGE"))
	output.WriteString("\n\n")
	output.WriteString("  ")
	output.WriteString(CommandStyle.Render("Interactive"))
	output.WriteString("   ")
	output.WriteString(DescStyle.Render("tnr report"))
	output.WriteString("\n")

	output.WriteString("  ")
	output.WriteString(CommandStyle.Render("With flags"))
	output.WriteString("   ")
	output.WriteString(DescStyle.Render("tnr report --subject <text> --details <text>"))
	output.WriteString("\n\n")

	// Examples Section
	output.WriteString(SectionStyle.Render("● EXAMPLES"))
	output.WriteString("\n\n")
	for _, example := range []struct{ comment, command string }{
		{"# Step by step, with a review before sending", "tnr report"},
		{"# Suggest an improvement", "tnr report --kind feedback"},
		{"# Report a problem with instance 42, details from a file", "tnr report --instance 42 --details-file ./notes.txt"},
		{"# Send without the review, for example from a script", `tnr report --subject "GPU not found" --details-file ./notes.txt --yes`},
	} {
		output.WriteString("  ")
		output.WriteString(ExampleStyle.Render(example.comment))
		output.WriteString("\n")
		output.WriteString("  ")
		output.WriteString(CommandTextStyle.Render(example.command))
		output.WriteString("\n\n")
	}

	// Flags Section
	output.WriteString(SectionStyle.Render("● FLAGS"))
	output.WriteString("\n\n")
	for _, flag := range []struct{ name, desc string }{
		{"--kind", "Report type: bug or feedback"},
		{"--subject", "One-line summary, up to 200 characters"},
		{"--details", "Report text, up to 10,000 characters"},
		{"--details-file", "Read the report text from a file, or - for standard input"},
		{"--instance", "Instance the report is about"},
		{"--sandbox", "Sandbox the report is about"},
		{"--component", "Area: cli, console, api, mcp, virtualization or unknown"},
		{"--occurred-at", "When it happened, as an RFC 3339 time"},
		{"--dry-run", "Preview the report without signing in or sending it"},
		{"--yes, -y", "Send without the review step"},
	} {
		output.WriteString("  ")
		output.WriteString(FlagStyle.Render(flag.name))
		output.WriteString("   ")
		output.WriteString(DescStyle.Render(flag.desc))
		output.WriteString("\n")
	}
	output.WriteString("\n")

	fmt.Fprint(os.Stdout, output.String())
}
