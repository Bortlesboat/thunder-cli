package helpmenus

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func RenderLogoutHelp(cmd *cobra.Command) {
	InitHelpStyles(os.Stdout)

	var output strings.Builder

	header := HelpHeader("LOGOUT COMMAND", "Remove local Thunder Compute authentication")

	output.WriteString(HeaderStyle.Render(header))

	// Usage Section
	output.WriteString(SectionStyle.Render("● USAGE"))
	output.WriteString("\n\n")
	output.WriteString("  ")
	output.WriteString(CommandStyle.Render("Logout"))
	output.WriteString("   ")
	output.WriteString(DescStyle.Render("tnr logout"))
	output.WriteString("\n\n")

	// Options Section
	output.WriteString(SectionStyle.Render("● OPTIONS"))
	output.WriteString("\n\n")
	output.WriteString("  ")
	output.WriteString(CommandTextStyle.Render("--revoke"))
	output.WriteString("   ")
	output.WriteString(DescStyle.Render("Revoke this member's Thunder CLI access before removing local credentials"))
	output.WriteString("\n\n")
	output.WriteString(DescStyle.Render("If revocation cannot be confirmed, saved credentials are kept. Use tnr logout to remove them locally. If the saved login can no longer authenticate, sign in to Console Settings > Connected apps to check or revoke this member's CLI access."))
	output.WriteString("\n\n")

	// Examples Section
	output.WriteString(SectionStyle.Render("● EXAMPLES"))
	output.WriteString("\n\n")
	output.WriteString("  ")
	output.WriteString(ExampleStyle.Render("# Remove saved credentials locally"))
	output.WriteString("\n")
	output.WriteString("  ")
	output.WriteString(CommandTextStyle.Render("tnr logout"))
	output.WriteString("\n\n")
	output.WriteString("  ")
	output.WriteString(ExampleStyle.Render("# Revoke this member's CLI access without affecting other organization members"))
	output.WriteString("\n")
	output.WriteString("  ")
	output.WriteString(CommandTextStyle.Render("tnr logout --revoke"))
	output.WriteString("\n\n")

	fmt.Fprint(os.Stdout, output.String())
}
