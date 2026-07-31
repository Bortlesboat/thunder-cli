package tui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInlineRuntimeWrapsANSITextToTerminalWidth(t *testing.T) {
	runtime := newInlineRuntime(true)
	runtime.update(tea.WindowSizeMsg{Width: 12, Height: 5})

	wrapped := runtime.wrap("\x1b[1mA styled message that must remain complete\x1b[0m")
	for _, line := range strings.Split(wrapped, "\n") {
		assert.LessOrEqual(t, ansi.StringWidth(line), 12)
	}
	assert.Equal(t, "A styled message that must remain complete", strings.Join(strings.Fields(stripANSI(wrapped)), " "))
}

func TestInlineRuntimeViewOwnsAtMostOneCellAcrossResizes(t *testing.T) {
	runtime := newInlineRuntime(true)

	for _, width := range []int{80, 1, 20, 0, 120} {
		runtime.update(tea.WindowSizeMsg{Width: width, Height: 24})
		view := runtime.view("\x1b[1m⠋\x1b[0m message\nsecond line", false)

		assert.False(t, view.AltScreen)
		assert.NotContains(t, view.Content, "\n")
		assert.LessOrEqual(t, ansi.StringWidth(view.Content), 1)
	}
}

func TestBusyModelCancellationIsImmediateForReadOnlyWork(t *testing.T) {
	release := make(chan struct{})
	task := newBackgroundTask(func() error {
		<-release
		return nil
	})
	defer close(release)

	model := NewBusyModel("Fetching instances...")
	model.task = task
	updated, cmd := model.Update(tea.KeyPressMsg(tea.Key{Code: 'q', Text: "q"}))
	result := updated.(BusyModel)

	assert.True(t, result.Interrupted)
	assert.True(t, result.Quitting)
	require.NotNil(t, cmd)
	select {
	case <-task.done:
		t.Fatal("read-only task unexpectedly stopped when its renderer was cancelled")
	default:
	}
}

func TestBusyModelTranscriptShowsCancellationHint(t *testing.T) {
	model := NewBusyModel("Fetching instances...")
	transcript := stripANSI(model.transcript())

	assert.Contains(t, transcript, "Fetching instances...")
	assert.Contains(t, transcript, "Esc/Q: Quit")
}

func TestProgressModelDoesNotAbandonMutationOnCancellationKey(t *testing.T) {
	release := make(chan struct{})
	task := newBackgroundTask(func() error {
		<-release
		return nil
	})
	defer close(release)

	model := newProgressModel(task)
	updated, cmd := model.Update(tea.KeyPressMsg(tea.Key{Code: 'q', Text: "q"}))
	result := updated.(progressModel)

	assert.True(t, result.cancellationRequested)
	assert.False(t, result.done)
	require.NotNil(t, cmd)
	select {
	case <-task.done:
		t.Fatal("mutation unexpectedly stopped when cancellation was requested")
	default:
	}
}

func TestRunProgressOwnsProgramAndPrintsTranscript(t *testing.T) {
	var out bytes.Buffer

	err := runProgress(
		"Creating instance...",
		&out,
		func() error { return nil },
		func() string { return "Instance created" },
		tea.WithInput(nil),
		tea.WithoutSignals(),
	)

	require.NoError(t, err)
	assert.Contains(t, stripANSI(out.String()), "Creating instance...")
	assert.Contains(t, stripANSI(out.String()), "Instance created")
}

func TestRunProgressWaitsForMutationAfterRendererStops(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	returned := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	go func() {
		returned <- runProgress(
			"Deleting instance...",
			io.Discard,
			func() error {
				close(started)
				<-release
				return nil
			},
			nil,
			tea.WithInput(nil),
			tea.WithContext(ctx),
		)
	}()

	<-started
	select {
	case err := <-returned:
		t.Fatalf("runner returned before mutation completed: %v", err)
	default:
	}

	close(release)
	select {
	case err := <-returned:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not return after mutation completed")
	}
}

func TestRunProgressReturnsMutationError(t *testing.T) {
	want := errors.New("mutation failed")
	err := runProgress(
		"Modifying instance...",
		io.Discard,
		func() error { return want },
		nil,
		tea.WithInput(nil),
		tea.WithoutSignals(),
	)

	assert.ErrorIs(t, err, want)
}
