package tui

import (
	"fmt"
	"io"
	"os"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

type progressResultMsg struct{}

// progressModel is deliberately private. Command packages invoke RunProgress
// instead of constructing a Bubble Tea program or depending on its final model
// type.
type progressModel struct {
	runtime               inlineRuntime
	spinner               spinner.Model
	task                  *backgroundTask
	done                  bool
	cancellationRequested bool
}

func newProgressModel(task *backgroundTask) progressModel {
	return progressModel{
		runtime: newInlineRuntime(true),
		spinner: NewPrimarySpinnerV2(),
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
	case tea.WindowSizeMsg:
		m.runtime.update(msg)
		return m, nil

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

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "q", "Q", "esc":
			if m.cancellationRequested {
				return m, nil
			}
			m.cancellationRequested = true
			return m, m.runtime.print("Operation is already in progress; waiting for it to finish.")
		}
	}

	return m, nil
}

func (m progressModel) View() tea.View {
	return m.runtime.view(m.spinner.View(), m.done)
}

// RunProgress runs a mutating operation with a one-cell v2 progress renderer.
// It owns the Bubble Tea program and does not return until the operation has
// completed, even if the user interrupts the renderer. API mutations cannot be
// safely abandoned because their clients do not expose cancellation-aware
// methods.
func RunProgress(message string, action func() error, renderSuccess func() string) error {
	InitCommonStyles(os.Stdout)
	return runProgress(message, os.Stdout, action, renderSuccess)
}

func runProgress(message string, out io.Writer, action func() error, renderSuccess func() string, options ...tea.ProgramOption) error {
	if err := writeTranscript(out, LabelStyle().Bold(false).Render(message)); err != nil {
		return fmt.Errorf("write progress transcript: %w", err)
	}

	task := newBackgroundTask(action)
	model := newProgressModel(task)
	programOptions := append([]tea.ProgramOption{tea.WithOutput(out)}, options...)

	// The operation result is authoritative. If the renderer exits early, wait
	// for the mutation so callers never report cancellation while it continues.
	_, _ = runInlineProgram(model, programOptions...)
	if err := task.wait(); err != nil {
		return err
	}

	if renderSuccess != nil {
		if err := writeTranscript(out, renderSuccess()); err != nil {
			return fmt.Errorf("write progress result: %w", err)
		}
	}
	return nil
}
