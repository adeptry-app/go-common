package redis

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func newTestClient(t *testing.T) (*goredis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	return goredis.NewClient(&goredis.Options{Addr: mr.Addr()}), mr
}

// bump charges the window and fails the test on error.
func bump(t *testing.T, client *goredis.Client, key string, window time.Duration) (count, ttl int64) {
	t.Helper()
	count, ttl, err := Bump(context.Background(), client, key, window)
	if err != nil {
		t.Fatalf("Bump() error = %v", err)
	}
	return count, ttl
}

func TestBump_CountsAndArmsTheWindow(t *testing.T) {
	client, mr := newTestClient(t)

	for want := int64(1); want <= 3; want++ {
		if got, ttl := bump(t, client, "k", time.Minute); got != want || ttl != 60 {
			t.Errorf("Bump() = (%d, %d), want (%d, 60)", got, ttl, want)
		}
	}

	if ttl := mr.TTL("k"); ttl != time.Minute {
		t.Errorf("TTL = %v, want %v", ttl, time.Minute)
	}
	mr.FastForward(time.Minute + time.Second)
	if got, _ := bump(t, client, "k", time.Minute); got != 1 {
		t.Errorf("after the window Bump() = %d, want 1", got)
	}
}

// The failure this script exists to prevent: a counter that outlived its window
// climbs to the cap forever and locks its subject out permanently.
func TestBump_ReArmsACounterThatLostItsTTL(t *testing.T) {
	client, mr := newTestClient(t)

	bump(t, client, "k", time.Minute)
	if err := client.Persist(context.Background(), "k").Err(); err != nil {
		t.Fatalf("Persist() error = %v", err)
	}
	if ttl := mr.TTL("k"); ttl != 0 {
		t.Fatalf("precondition: TTL = %v, want none", ttl)
	}

	bump(t, client, "k", time.Minute)
	if ttl := mr.TTL("k"); ttl != time.Minute {
		t.Errorf("TTL = %v, want the window re-armed", ttl)
	}
}

// EXPIRE 0 deletes the key, so a truncated window would leave every Bump
// returning 1 and the limiter permanently open.
func TestBump_RoundsASubSecondWindowUp(t *testing.T) {
	client, mr := newTestClient(t)

	for want := int64(1); want <= 2; want++ {
		if got, _ := bump(t, client, "k", 500*time.Millisecond); got != want {
			t.Errorf("Bump() = %d, want %d", got, want)
		}
	}
	if ttl := mr.TTL("k"); ttl != time.Second {
		t.Errorf("TTL = %v, want %v", ttl, time.Second)
	}
}

// Retry-After is built from this, so it must shrink as the window drains.
func TestBump_ReportsTheRemainingWindow(t *testing.T) {
	client, mr := newTestClient(t)

	bump(t, client, "k", time.Minute)
	mr.FastForward(45 * time.Second)

	if _, ttl := bump(t, client, "k", time.Minute); ttl != 15 {
		t.Errorf("ttl = %d, want 15", ttl)
	}
}

func TestRefund(t *testing.T) {
	client, _ := newTestClient(t)

	bump(t, client, "k", time.Minute)
	if err := Refund(context.Background(), client, "k"); err != nil {
		t.Fatalf("Refund() error = %v", err)
	}
	if got, _ := bump(t, client, "k", time.Minute); got != 1 {
		t.Errorf("after refund Bump() = %d, want 1", got)
	}
}

// A DECR on an expired key would recreate it at -1 with no window, so the
// bucket would never reach its cap again.
func TestRefund_DoesNotResurrectAnExpiredBucket(t *testing.T) {
	client, mr := newTestClient(t)

	if err := Refund(context.Background(), client, "gone"); err != nil {
		t.Fatalf("Refund() error = %v", err)
	}
	if mr.Exists("gone") {
		t.Error("Refund recreated a bucket that had expired")
	}
}

func TestWindowSeconds(t *testing.T) {
	tests := []struct {
		window time.Duration
		want   int64
	}{
		{-time.Second, 1},
		{0, 1},
		{time.Nanosecond, 1},
		{999 * time.Millisecond, 1},
		{time.Second, 1},
		{1500 * time.Millisecond, 2},
		{time.Minute, 60},
	}

	for _, tt := range tests {
		if got := WindowSeconds(tt.window); got != tt.want {
			t.Errorf("WindowSeconds(%v) = %d, want %d", tt.window, got, tt.want)
		}
	}
}
