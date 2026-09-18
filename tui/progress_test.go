package tui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBusyModelCancellationIsImmediateForReadOnlyWork(t *testing.T) {
	release := make(chan struct{})
	task := newBackgroundTask(func() error {
		<-release
		return nil
	})
	defer close(release)

	model := NewBusyModel("Fetching instances...")
	model.task = task
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
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

func TestBusyModelDismissesMessageAndCancellationHint(t *testing.T) {
	model := NewBusyModel("Fetching instances...")
	view := stripANSI(model.View())

	assert.Contains(t, view, "Fetching instances...")
	assert.Contains(t, view, "Esc/Q: Quit")

	updated, _ := model.Update(BusyDoneMsg{})
	assert.Empty(t, updated.(BusyModel).View())
}

func TestProgressModelDoesNotAbandonMutationOnCancellationKey(t *testing.T) {
	release := make(chan struct{})
	task := newBackgroundTask(func() error {
		<-release
		return nil
	})
	defer close(release)

	model := newProgressModel("Creating instance...", task)
	assert.Contains(t, stripANSI(model.View()), "Creating instance...")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	result := updated.(progressModel)

	assert.True(t, result.cancellationRequested)
	assert.False(t, result.done)
	assert.Nil(t, cmd)
	assert.Contains(t, stripANSI(result.View()), "waiting for it to finish")
	select {
	case <-task.done:
		t.Fatal("mutation unexpectedly stopped when cancellation was requested")
	default:
	}
}

func TestRunProgressPrintsSuccess(t *testing.T) {
	var out bytes.Buffer

	err := runProgress(
		"Creating instance...",
		&out,
		func() error { return nil },
		func() string { return "Instance created" },
		tea.WithInput(nil),
		tea.WithoutSignalHandler(),
	)

	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(stripANSI(out.String()), "Instance created"))
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
		tea.WithoutSignalHandler(),
	)

	assert.ErrorIs(t, err, want)
}
