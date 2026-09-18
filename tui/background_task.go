package tui

import (
	"fmt"
	"runtime/debug"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

// backgroundTask starts work independently of Bubble Tea's command lifecycle.
// Bubble Tea intentionally does not wait for long-running commands when a
// program exits, so mutation runners use this completion signal to guarantee
// they never return while an API mutation is still in flight.
type backgroundTask struct {
	done chan struct{}

	mu  sync.RWMutex
	err error
}

func newBackgroundTask(fn func() error) *backgroundTask {
	task := &backgroundTask{done: make(chan struct{})}
	go func() {
		defer close(task.done)
		defer func() {
			if recovered := recover(); recovered != nil {
				task.setError(fmt.Errorf("terminal task panicked: %v\n%s", recovered, debug.Stack()))
			}
		}()
		task.setError(fn())
	}()
	return task
}

func (t *backgroundTask) setError(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.err = err
}

func (t *backgroundTask) wait() error {
	<-t.done
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.err
}

func (t *backgroundTask) command(msg func(error) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return msg(t.wait())
	}
}
