package helpmenus

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func RenderLoginHelp(cmd *cobra.Command) {
	InitHelpStyles(os.Stdout)

	var output strings.Builder

	header := HelpHeader("LOGIN COMMAND", "Authenticate with Thunder Compute")

	output.WriteString(HeaderStyle.Render(header))

	// Usage Section
	output.WriteString(SectionStyle.Render("● USAGE"))
	output.WriteString("\n\n")
	output.WriteString("  ")
	output.WriteString(CommandStyle.Render("Browser"))
	output.WriteString("   ")
	output.WriteString(DescStyle.Render("tnr login"))
	output.WriteString("\n")

	output.WriteString("  ")
	output.WriteString(CommandStyle.Render("Token"))
	output.WriteString("   ")
	output.WriteString(DescStyle.Render("tnr login --token <your_token_id>"))
	output.WriteString("\n\n")

	// Examples Section
	output.WriteString(SectionStyle.Render("● EXAMPLES"))
	output.WriteString("\n\n")
	output.WriteString("  ")
	output.WriteString(ExampleStyle.Render("# Interactive browser authentication"))
	output.WriteString("\n")
	output.WriteString("  ")
	output.WriteString(CommandTextStyle.Render("tnr login"))
	output.WriteString("\n\n")

	output.WriteString("  ")
	output.WriteString(ExampleStyle.Render("# Direct token authentication"))
	output.WriteString("\n")
	output.WriteString("  ")
	output.WriteString(CommandTextStyle.Render("tnr login --token abc123xyz789"))
	output.WriteString("\n\n")

	output.WriteString("  ")
	output.WriteString(ExampleStyle.Render("# Sign in from SSH or a headless machine"))
	output.WriteString("\n  ")
	output.WriteString(CommandTextStyle.Render("tnr login --manual"))
	output.WriteString("\n\n")

	// Flags Section
	output.WriteString(SectionStyle.Render("● FLAGS"))
	output.WriteString("\n\n")

	output.WriteString("  ")
	output.WriteString(FlagStyle.Render("--token"))
	output.WriteString("   ")
	output.WriteString(DescStyle.Render("Authenticate directly with a token instead of opening browser"))
	output.WriteString("\n\n")

	output.WriteString("  ")
	output.WriteString(FlagStyle.Render("--manual"))
	output.WriteString("   ")
	output.WriteString(DescStyle.Render("Paste a token to finish login; selected automatically over SSH"))
	output.WriteString("\n  ")
	output.WriteString(DescStyle.Render("Use --manual=false to try automatic browser login over SSH."))
	output.WriteString("\n\n")

	fmt.Fprint(os.Stdout, output.String())
}
