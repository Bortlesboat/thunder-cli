package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Thunder-Compute/thunder-cli/tui/theme"
)

type updateStyles struct {
	title   lipgloss.Style
	version lipgloss.Style
	arrow   lipgloss.Style
	pmBox   lipgloss.Style
	command lipgloss.Style
	help    lipgloss.Style
	label   lipgloss.Style
}

func newUpdateStyles() updateStyles {
	return updateStyles{
		title:   TitleStyle(),
		version: LabelStyle(),
		arrow:   SubtleTextStyle(),
		pmBox: WarningStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(theme.WarningColor).
			Padding(1, 2),
		command: LabelStyle(),
		help:    HelpStyle(),
		label:   LabelStyle(),
	}
}

func RenderUpToDate(version string) string {
	InitCommonStyles(os.Stdout)
	return SuccessStyle().Render(fmt.Sprintf("✓ tnr is already up-to-date (%s)", version))
}

func RenderUpdateAvailable(currentVer, latestVer string) string {
	InitCommonStyles(os.Stdout)
	styles := newUpdateStyles()
	return fmt.Sprintf("%s %s %s %s",
		WarningStyle().Render("⚠ Update available:"),
		styles.version.Render(currentVer),
		styles.arrow.Render("→"),
		styles.version.Render(latestVer))
}

func RenderUpdating(currentVer, latestVer string) string {
	InitCommonStyles(os.Stdout)
	styles := newUpdateStyles()
	return fmt.Sprintf("%s %s%s %s%s",
		theme.Body().Render("Currently on version"),
		styles.version.Render(currentVer),
		theme.Body().Render(", latest version"),
		styles.version.Render(latestVer),
		theme.Body().Render(", installing update..."))
}

func RenderPMInstructions(pm, currentVer, latestVer string) string {
	InitCommonStyles(os.Stdout)
	styles := newUpdateStyles()

	var content strings.Builder
	content.WriteString(fmt.Sprintf("Update available: %s → %s\n\n",
		styles.version.Render(currentVer),
		styles.version.Render(latestVer)))

	var pmName, command string
	switch pm {
	case "homebrew":
		pmName = "Homebrew"
		command = "brew update && brew upgrade tnr"
	case "scoop":
		pmName = "Scoop"
		command = "scoop update tnr"
	case "winget":
		pmName = "Windows Package Manager"
		command = "winget upgrade Thunder.tnr"
	default:
		pmName = "a package manager"
		command = "brew upgrade tnr"
	}

	content.WriteString(fmt.Sprintf("This installation is managed by %s.\n", pmName))
	content.WriteString(fmt.Sprintf("Run: %s", styles.command.Render(command)))

	return styles.pmBox.Render(content.String())
}

func RenderUpdateSuccess() string {
	InitCommonStyles(os.Stdout)
	return SuccessStyle().Render("✓ Update successful!")
}

func RenderUpdateFailed(err error, releaseURL string) string {
	InitCommonStyles(os.Stdout)
	var content strings.Builder
	content.WriteString(ErrorStyle().Render(fmt.Sprintf("✗ Update failed: %v", err)))
	content.WriteString("\n")
	content.WriteString(HelpStyle().Render(fmt.Sprintf("You can download the latest version from: %s", releaseURL)))
	return content.String()
}
