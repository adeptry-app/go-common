package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// The case a worker needs: its consumer died, so it cancels the context instead
// of exiting under this boundary, and Run has to come back.
func TestRun_CancelledContextStopsServer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, http.NewServeMux(), DefaultConfig("0"), discardLogger())
	}()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}

// Cancelling is only worth anything if the release hooks still fire.
func TestRunWithCleanup_CleanupRunsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cleaned := false
	err := RunWithCleanup(ctx, http.NewServeMux(), DefaultConfig("0"), discardLogger(), func() {
		cleaned = true
	})

	if err != nil {
		t.Fatalf("RunWithCleanup() = %v, want nil", err)
	}
	if !cleaned {
		t.Error("cleanup did not run after the context was cancelled")
	}
}

func TestRun_NilHandlerIsRefused(t *testing.T) {
	if err := Run(context.Background(), nil, DefaultConfig("0"), discardLogger()); err == nil {
		t.Error("Run(nil handler) = nil, want an error")
	}
}
