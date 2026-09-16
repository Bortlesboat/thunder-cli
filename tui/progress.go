package tui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

type progressResultMsg struct{}

// progressModel is deliberately private. Command packages invoke RunProgress
// instead of constructing a Bubble Tea program or depending on its final model
// type.
type progressModel struct {
	message               string
	spinner               spinner.Model
	task                  *backgroundTask
	done                  bool
	cancellationRequested bool
}

func newProgressModel(message string, task *backgroundTask) progressModel {
	return progressModel{
		message: message,
		spinner: NewPrimarySpinner(),
		task:    task,
	}
}

func (m progressModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.task.command(func(error) tea.Msg {
		return progressResultMsg{}
	}))
}

func (m progressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.done {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case progressResultMsg:
		m.done = true
		return m, tea.Quit

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "Q", "esc":
			m.cancellationRequested = true
			return m, nil
		}
	}

	return m, nil
}

func (m progressModel) View() string {
	if m.done {
		return ""
	}
	message := m.message
	if m.cancellationRequested {
		message += "\n Operation is already in progress; waiting for it to finish."
	}
	return fmt.Sprintf("\n %s %s\n", m.spinner.View(), message)
}

// RunProgress waits for the mutation's result even if its renderer is interrupted.
func RunProgress(message string, action func() error, renderSuccess func() string) error {
	InitCommonStyles(os.Stdout)
	return runProgress(message, os.Stdout, action, renderSuccess)
}

func runProgress(message string, out io.Writer, action func() error, renderSuccess func() string, options ...tea.ProgramOption) error {
	task := newBackgroundTask(action)
	model := newProgressModel(message, task)
	programOptions := append([]tea.ProgramOption{tea.WithOutput(out)}, options...)

	// The operation result is authoritative. If the renderer exits early, wait
	// for the mutation so callers never report cancellation while it continues.
	_, _ = tea.NewProgram(model, programOptions...).Run()
	if err := task.wait(); err != nil {
		return err
	}

	if renderSuccess != nil {
		if content := renderSuccess(); out != nil && content != "" {
			if _, err := fmt.Fprintln(out, strings.TrimSuffix(content, "\n")); err != nil {
				return fmt.Errorf("write progress result: %w", err)
			}
		}
	}
	return nil
}
