package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type BusyDoneMsg struct{}

type BusyModel struct {
	text        string
	spin        spinner.Model
	Quitting    bool
	Interrupted bool
	task        *backgroundTask

	styles busyStyles
}

type busyStyles struct {
	text lipgloss.Style
	help lipgloss.Style
}

func newBusyStyles() busyStyles {
	return busyStyles{
		text: LabelStyle().Bold(false),
		help: HelpStyle(),
	}
}

func NewBusyModel(text string) BusyModel {
	InitCommonStyles(os.Stdout)
	return BusyModel{
		text:   text,
		spin:   NewPrimarySpinner(),
		styles: newBusyStyles(),
	}
}

func (m BusyModel) Init() tea.Cmd {
	if m.task != nil {
		return tea.Batch(m.spin.Tick, m.task.command(func(error) tea.Msg {
			return BusyDoneMsg{}
		}))
	}
	return m.spin.Tick
}

func (m BusyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case BusyDoneMsg:
		m.Quitting = true
		return m, tea.Quit
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "Q", "esc", "ctrl+c":
			m.Interrupted = true
			m.Quitting = true
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m BusyModel) View() string {
	if m.Quitting {
		return ""
	}
	return m.spin.View() + " " + m.styles.text.Render(m.text) + "\n" + m.styles.help.Render("Esc/Q: Quit\n")
}

// RunWithBusySpinner shows a spinner while fn executes, then dismisses it.
// In non-interactive mode (no TTY), it skips the TUI spinner and runs fn synchronously.
//
// If the user cancels (q/esc/ctrl+c) it returns context.Canceled immediately
// while fn may still be running, so callers must not read fn's outputs unless
// the returned error is nil. This immediate-cancellation runner is only safe
// for read-only work. Mutating operations must use RunProgress, which waits
// for the operation's actual result before returning.
func RunWithBusySpinner(message string, out io.Writer, fn func() error) error {
	if !IsInteractive() {
		fmt.Fprintf(os.Stderr, "%s\n", message)
		return fn()
	}

	busy := NewBusyModel(message)
	busy.task = newBackgroundTask(fn)

	finalModel, runErr := tea.NewProgram(busy, tea.WithOutput(out), tea.WithoutSignalHandler()).Run()
	if m, ok := finalModel.(BusyModel); ok && m.Interrupted {
		return context.Canceled
	}
	if errors.Is(runErr, tea.ErrInterrupted) {
		return context.Canceled
	}

	// A renderer failure must not fail the command. The read-only operation's
	// result is still authoritative.
	return busy.task.wait()
}
