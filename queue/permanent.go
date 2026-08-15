package queue

import "errors"

// ErrPermanent marks a handler error as permanent: the message can never be
// processed successfully (e.g. malformed payload, missing referenced entity).
// The consumer sends such messages straight to the DLQ instead of burning
// retry attempts.
//
// Handlers can either wrap an error with Permanent():
//
//	return queue.Permanent(fmt.Errorf("unmarshal event: %w", err))
//
// or wrap the sentinel directly:
//
//	return fmt.Errorf("unknown message type %q: %w", t, queue.ErrPermanent)
//
// Both forms satisfy errors.Is(err, queue.ErrPermanent).
var ErrPermanent = errors.New("permanent failure")

// Permanent wraps err so the consumer routes the message straight to the DLQ.
// Returns nil if err is nil. The wrapped error is available via errors.Unwrap.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

type permanentError struct {
	err error
}

func (e *permanentError) Error() string { return e.err.Error() }

func (e *permanentError) Unwrap() error { return e.err }

// Is reports true for ErrPermanent so errors.Is(err, ErrPermanent) matches.
func (e *permanentError) Is(target error) bool { return target == ErrPermanent }

// WithAttempt wraps a transient error with the business attempt it failed on,
// which the consumer uses as the retry-ladder index. Returns nil if err is nil.
//
//	return queue.WithAttempt(claimed.Attempt, err)
//
// The attempt comes from the claim (the row's own counter), never from the
// delivery's receive count, so an operational redelivery cannot skip a rung.
func WithAttempt(attempt int, err error) error {
	if err == nil {
		return nil
	}
	return &attemptError{attempt: attempt, err: err}
}

// AttemptOf reports the business attempt recorded by WithAttempt, if any.
func AttemptOf(err error) (int, bool) {
	var attemptErr *attemptError
	if errors.As(err, &attemptErr) {
		return attemptErr.attempt, true
	}
	return 0, false
}

type attemptError struct {
	attempt int
	err     error
}

func (e *attemptError) Error() string { return e.err.Error() }

func (e *attemptError) Unwrap() error { return e.err }
