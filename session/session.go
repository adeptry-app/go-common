// Package session owns the Redis layout that decides whether a browser access
// token is still backed by a live session and an authorization version its user
// has not outgrown. One definition, so a key rename cannot leave one service
// reading what another stopped writing.
package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/adeptry-app/go-common/jwt"
	"github.com/adeptry-app/go-common/middleware"
)

// Key patterns for the two pieces of state a validator reads.
const (
	refreshTokenPattern = "refresh_token:%d:%s"
	authVersionPattern  = "auth_version:%d"
)

// RefreshTokenKey holds a session's current refresh token. Its presence is what
// makes the session live: logout and revocation delete it.
func RefreshTokenKey(userID int64, sessionID string) string {
	return fmt.Sprintf(refreshTokenPattern, userID, sessionID)
}

// AuthVersionKey holds the user's current authorization version.
func AuthVersionKey(userID int64) string {
	return fmt.Sprintf(authVersionPattern, userID)
}

// AuthVersion reads the version to stamp into a token being minted. A user whose
// authorization has never been revoked has no key, which reads as version zero.
func AuthVersion(ctx context.Context, client goredis.Cmdable, userID int64) (int64, error) {
	version, err := client.Get(ctx, AuthVersionKey(userID)).Int64()
	if errors.Is(err, goredis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read auth version: %w", err)
	}
	return version, nil
}

// BumpAuthVersion invalidates every access token minted for the user so far,
// which is how a role downgrade or a password change takes effect before those
// tokens expire. Sessions themselves survive; the next refresh restamps them.
// ttl must outlive any access token still in circulation.
func BumpAuthVersion(ctx context.Context, client goredis.Cmdable, userID int64, ttl time.Duration) (int64, error) {
	key := AuthVersionKey(userID)

	version, err := client.Incr(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("bump auth version: %w", err)
	}
	if err := client.Expire(ctx, key, ttl).Err(); err != nil {
		return 0, fmt.Errorf("bump auth version: %w", err)
	}
	return version, nil
}

// Store answers middleware.SessionValidator from Redis.
type Store struct {
	client goredis.Cmdable
}

// NewStore builds the validator every token-consuming service registers.
// Panics if client is nil to fail fast on misconfiguration.
func NewStore(client goredis.Cmdable) *Store {
	if client == nil {
		panic("session: cannot use a nil redis client")
	}
	return &Store{client: client}
}

// ValidateSession rejects a token whose session is gone or whose authorization
// version the user has outgrown. Both reads go out as one round trip.
func (s *Store) ValidateSession(ctx context.Context, userID int64, session jwt.Session) error {
	pipe := s.client.Pipeline()
	live := pipe.Exists(ctx, RefreshTokenKey(userID, session.ID))
	current := pipe.Get(ctx, AuthVersionKey(userID))

	// A missing version key is the common case, not a failure.
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, goredis.Nil) {
		return fmt.Errorf("read session state: %w", err)
	}

	if live.Val() == 0 {
		return middleware.ErrSessionRevoked
	}

	version, err := current.Int64()
	if errors.Is(err, goredis.Nil) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read auth version: %w", err)
	}
	if session.AuthVersion < version {
		return middleware.ErrSessionRevoked
	}
	return nil
}
