package outputmode

import (
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func init() {
	// Go initializes ready packages in import-path order. The uppercase T in
	// Thunder-Compute puts this package before Bubble Tea's terminal query.
	if JSONRequested(os.Args[1:]) {
		lipgloss.SetHasDarkBackground(true)
	}
}

func JSONRequested(args []string) bool {
	requested := false
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--json" {
			requested = true
			continue
		}
		if !strings.HasPrefix(arg, "--json=") {
			continue
		}
		parsed, err := strconv.ParseBool(strings.TrimPrefix(arg, "--json="))
		requested = err != nil || parsed
	}
	return requested
}
