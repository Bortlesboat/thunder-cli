package tui

import (
	"fmt"
	"io"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// inlineRuntime contains the resize and transcript policy shared by inline v2
// programs. Persistent content is printed above the renderer; the renderer
// itself owns at most one terminal cell so terminal reflow cannot strand old
// frames in scrollback.
type inlineRuntime struct {
	width      int
	transcript bool
}

func newInlineRuntime(transcript bool) inlineRuntime {
	return inlineRuntime{transcript: transcript}
}

func (r *inlineRuntime) update(msg tea.Msg) bool {
	size, ok := msg.(tea.WindowSizeMsg)
	if !ok {
		return false
	}
	r.width = size.Width
	return true
}

func (r inlineRuntime) wrap(text string) string {
	if r.width <= 0 {
		return text
	}
	return ansi.Wrap(text, r.width, "")
}

func (r inlineRuntime) view(liveCell string, quitting bool) tea.View {
	if quitting || r.width < 2 {
		return tea.NewView("")
	}

	line, _, _ := strings.Cut(liveCell, "\n")
	return tea.NewView(ansi.Truncate(line, 1, ""))
}

func (r inlineRuntime) print(content string) tea.Cmd {
	return tea.Println(strings.TrimSuffix(content, "\n"))
}

func (r inlineRuntime) withTranscript(cmd tea.Cmd, content string, changed bool) tea.Cmd {
	if !r.transcript || !changed {
		return cmd
	}
	return tea.Batch(cmd, r.print(content))
}

func writeTranscript(out io.Writer, content string) error {
	if out == nil || content == "" {
		return nil
	}
	_, err := fmt.Fprintln(out, strings.TrimSuffix(content, "\n"))
	return err
}

type inlineQuitNow struct{}

// deferInlineQuit lets transcript-print commands reach the renderer before the
// program exits and restores the terminal.
func deferInlineQuit() tea.Cmd {
	return tea.Tick(time.Millisecond, func(time.Time) tea.Msg { return inlineQuitNow{} })
}

func newInlineProgram(model tea.Model, options ...tea.ProgramOption) *tea.Program {
	return tea.NewProgram(model, options...)
}

func runInlineProgram(model tea.Model, options ...tea.ProgramOption) (tea.Model, error) {
	return newInlineProgram(model, options...).Run()
}

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
