package queue

import (
	"errors"
	"fmt"
	"testing"
)

// =============================================================================
// Permanent Error Tests
// =============================================================================

func TestPermanent_NilReturnsNil(t *testing.T) {
	if Permanent(nil) != nil {
		t.Error("Permanent(nil) should return nil")
	}
}

func TestPermanent_MatchesErrPermanent(t *testing.T) {
	err := Permanent(errors.New("bad payload"))

	if !errors.Is(err, ErrPermanent) {
		t.Error("Permanent() error should match ErrPermanent via errors.Is")
	}
}

func TestPermanent_WrappedMatchesErrPermanent(t *testing.T) {
	err := fmt.Errorf("handler: %w", Permanent(errors.New("bad payload")))

	if !errors.Is(err, ErrPermanent) {
		t.Error("wrapped Permanent() error should match ErrPermanent via errors.Is")
	}
}

func TestPermanent_SentinelWrapMatches(t *testing.T) {
	err := fmt.Errorf("unknown type: %w", ErrPermanent)

	if !errors.Is(err, ErrPermanent) {
		t.Error("error wrapping ErrPermanent should match via errors.Is")
	}
}

func TestPermanent_PreservesMessage(t *testing.T) {
	inner := errors.New("bad payload")
	err := Permanent(inner)

	if err.Error() != "bad payload" {
		t.Errorf("Permanent().Error() = %q, want %q", err.Error(), "bad payload")
	}
}

func TestPermanent_Unwrap(t *testing.T) {
	inner := errors.New("bad payload")
	err := Permanent(inner)

	if !errors.Is(err, inner) {
		t.Error("Permanent() should unwrap to the inner error")
	}
}

func TestPermanent_OrdinaryErrorDoesNotMatch(t *testing.T) {
	err := errors.New("transient failure")

	if errors.Is(err, ErrPermanent) {
		t.Error("ordinary error should not match ErrPermanent")
	}
}

// =============================================================================
// Attempt Error Tests
// =============================================================================

func TestWithAttempt_NilReturnsNil(t *testing.T) {
	if WithAttempt(1, nil) != nil {
		t.Error("WithAttempt(_, nil) should return nil")
	}
}

func TestWithAttempt_PreservesMessageAndUnwraps(t *testing.T) {
	inner := errors.New("provider timeout")
	err := WithAttempt(2, inner)

	if err.Error() != "provider timeout" {
		t.Errorf("WithAttempt().Error() = %q, want %q", err.Error(), "provider timeout")
	}
	if !errors.Is(err, inner) {
		t.Error("WithAttempt() should unwrap to the inner error")
	}
}

func TestAttemptOf(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantAttempt int
		wantOK      bool
	}{
		{"nil error", nil, 0, false},
		{"plain error", errors.New("transient"), 0, false},
		{"attempt error", WithAttempt(3, errors.New("transient")), 3, true},
		{"wrapped attempt error", fmt.Errorf("handler: %w", WithAttempt(4, errors.New("transient"))), 4, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attempt, ok := AttemptOf(tt.err)

			if ok != tt.wantOK {
				t.Errorf("AttemptOf() ok = %v, want %v", ok, tt.wantOK)
			}
			if attempt != tt.wantAttempt {
				t.Errorf("AttemptOf() attempt = %d, want %d", attempt, tt.wantAttempt)
			}
		})
	}
}

func TestWithAttempt_DoesNotMatchErrPermanent(t *testing.T) {
	err := WithAttempt(1, errors.New("transient"))

	// The two wrappers mean opposite things; a transient error must not settle
	// to the DLQ because it carries an attempt.
	if errors.Is(err, ErrPermanent) {
		t.Error("attempt error should not match ErrPermanent")
	}
}
