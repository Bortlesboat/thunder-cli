package tui

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

type interruptMsg struct{}

type Program struct {
	*tea.Program
	interrupted bool
}

func NewProgram(model tea.Model, options ...tea.ProgramOption) *Program {
	p := &Program{}
	options = append(options, tea.WithoutSignalHandler(), tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg {
		switch msg.(type) {
		case interruptMsg, tea.InterruptMsg:
			msg = tea.KeyMsg{Type: tea.KeyCtrlC}
		}
		if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyCtrlC {
			p.interrupted = true
		}
		return msg
	}))
	p.Program = tea.NewProgram(model, options...)
	return p
}

func (p *Program) listenSignals() func() {
	signals := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		for {
			select {
			case <-signals:
				// Let the model clear its view or retain its final table before quitting.
				p.Send(interruptMsg{})
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(signals)
		close(done)
	}
}

func RunProgram(p *Program) (tea.Model, error) {
	stop := p.listenSignals()
	defer stop()
	model, err := p.Run()
	if err == nil && p.interrupted {
		err = context.Canceled
	}
	return model, err
}
