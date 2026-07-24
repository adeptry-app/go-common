package queue

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// StaleSweeper Tests
// =============================================================================

// recordingPublisher records the ids it was asked to publish and fails the
// ones listed in failIDs.
type recordingPublisher struct {
	mu       sync.Mutex
	accepted []int64
	failIDs  map[int64]bool
}

func (p *recordingPublisher) Publish(_ context.Context, message any) error {
	id, ok := message.(int64)
	if !ok {
		return errors.New("unexpected message type")
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failIDs[id] {
		return errors.New("publish failed")
	}
	p.accepted = append(p.accepted, id)
	return nil
}

func (p *recordingPublisher) published() []int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]int64(nil), p.accepted...)
}

// sweeperFor wires a sweeper that republishes the raw id as the event.
func sweeperFor(sweep func(ctx context.Context) ([]int64, error), pub EventPublisher) StaleSweeper {
	return StaleSweeper{
		Sweep:     sweep,
		Event:     func(id int64) any { return id },
		Publisher: pub,
		Kind:      "test row",
		Logger:    testLogger(),
	}
}

func staticSweep(ids ...int64) func(context.Context) ([]int64, error) {
	return func(context.Context) ([]int64, error) { return ids, nil }
}

func TestStaleSweeper_PassPublishesEverySweptID(t *testing.T) {
	pub := &recordingPublisher{}
	s := sweeperFor(staticSweep(7, 11, 13), pub)

	s.pass(context.Background())

	got := pub.published()
	want := []int64{7, 11, 13}
	if len(got) != len(want) {
		t.Fatalf("published %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("published %v, want %v", got, want)
		}
	}
}

func TestStaleSweeper_PassSkipsPublishOnSweepError(t *testing.T) {
	pub := &recordingPublisher{}
	s := sweeperFor(func(context.Context) ([]int64, error) {
		return []int64{7}, errors.New("query failed")
	}, pub)

	s.pass(context.Background())

	if got := pub.published(); len(got) != 0 {
		t.Errorf("published %v on sweep error, want none", got)
	}
}

func TestStaleSweeper_PassNothingStale(t *testing.T) {
	pub := &recordingPublisher{}
	s := sweeperFor(staticSweep(), pub)

	s.pass(context.Background())

	if got := pub.published(); len(got) != 0 {
		t.Errorf("published %v with nothing stale, want none", got)
	}
}

func TestStaleSweeper_PassContinuesAfterPublishError(t *testing.T) {
	pub := &recordingPublisher{failIDs: map[int64]bool{11: true}}
	s := sweeperFor(staticSweep(7, 11, 13), pub)

	s.pass(context.Background())

	got := pub.published()
	if len(got) != 2 || got[0] != 7 || got[1] != 13 {
		t.Errorf("published %v, want the two ids either side of the failure", got)
	}
}

func TestStaleSweeper_RunSweepsUntilContextCancelled(t *testing.T) {
	passes := make(chan struct{}, 8)
	pub := &recordingPublisher{}
	s := sweeperFor(func(context.Context) ([]int64, error) {
		// Non-blocking: a full buffer must not pin the sweeper goroutine, or
		// it never observes the cancellation below.
		select {
		case passes <- struct{}{}:
		default:
		}
		return []int64{7}, nil
	}, pub)
	s.Interval = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	for range 2 {
		select {
		case <-passes:
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatal("sweeper did not run a pass within 5s")
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestStaleSweeper_RunSweepsBeforeFirstTick(t *testing.T) {
	passes := make(chan struct{}, 1)
	s := sweeperFor(func(context.Context) ([]int64, error) {
		passes <- struct{}{}
		return nil, nil
	}, &recordingPublisher{})
	s.Interval = time.Hour // No tick can fire, so only the entry sweep can.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	select {
	case <-passes:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not sweep before the first tick")
	}
}

func TestStaleSweeper_RunAppliesDefaults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Zero Interval and nil Logger must fall back rather than panic; an
	// already-cancelled context returns before the first tick.
	s := StaleSweeper{
		Sweep:     staticSweep(7),
		Event:     func(id int64) any { return id },
		Publisher: &recordingPublisher{},
	}

	// Run never reaches a tick here, so exercise the nil-logger path directly.
	s.pass(context.Background())

	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return on a cancelled context")
	}
}
