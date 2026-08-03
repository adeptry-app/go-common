package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	"github.com/adeptry-app/go-common/jwt"
)

const testSessionID = "0e2a1f2c-1c4f-4a3f-9a2b-6d1f0b7c8e55"

func newTestStore(t *testing.T) (*Store, *miniredis.Miniredis) {
	t.Helper()

	mr := miniredis.RunT(t)
	return NewStore(goredis.NewClient(&goredis.Options{Addr: mr.Addr()})), mr
}

// live writes the key whose presence makes a session valid.
func live(t *testing.T, mr *miniredis.Miniredis, userID int64, sessionID string) {
	t.Helper()

	if err := mr.Set(RefreshTokenKey(userID, sessionID), "refresh-token"); err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

func TestValidateSession(t *testing.T) {
	tests := []struct {
		name    string
		seed    func(*testing.T, *miniredis.Miniredis)
		token   jwt.Session
		wantErr error
	}{
		{
			name:  "live session, nothing ever revoked",
			seed:  func(t *testing.T, mr *miniredis.Miniredis) { live(t, mr, 42, testSessionID) },
			token: jwt.Session{ID: testSessionID},
		},
		{
			name:    "session destroyed by logout",
			seed:    func(*testing.T, *miniredis.Miniredis) {},
			token:   jwt.Session{ID: testSessionID},
			wantErr: jwt.ErrSessionRevoked,
		},
		{
			name: "another user's live session",
			seed: func(t *testing.T, mr *miniredis.Miniredis) { live(t, mr, 43, testSessionID) },
			token: jwt.Session{
				ID: testSessionID,
			},
			wantErr: jwt.ErrSessionRevoked,
		},
		{
			name: "token stamped at the current version",
			seed: func(t *testing.T, mr *miniredis.Miniredis) {
				live(t, mr, 42, testSessionID)
				if err := mr.Set(AuthVersionKey(42), "3"); err != nil {
					t.Fatalf("seed version: %v", err)
				}
			},
			token: jwt.Session{ID: testSessionID, AuthVersion: 3},
		},
		{
			name: "token minted before the version was bumped",
			seed: func(t *testing.T, mr *miniredis.Miniredis) {
				live(t, mr, 42, testSessionID)
				if err := mr.Set(AuthVersionKey(42), "3"); err != nil {
					t.Fatalf("seed version: %v", err)
				}
			},
			token:   jwt.Session{ID: testSessionID, AuthVersion: 2},
			wantErr: jwt.ErrSessionRevoked,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, mr := newTestStore(t)
			tt.seed(t, mr)

			if err := store.ValidateSession(context.Background(), 42, tt.token); !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidateSession() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateSession_UnreachableRedisIsNotRevocation(t *testing.T) {
	store, mr := newTestStore(t)
	mr.Close()

	err := store.ValidateSession(context.Background(), 42, jwt.Session{ID: testSessionID})
	if err == nil {
		t.Fatal("ValidateSession() error = nil, want a transport error")
	}
	// The middleware answers 503 for this and 401 only for a real revocation.
	if errors.Is(err, jwt.ErrSessionRevoked) {
		t.Errorf("ValidateSession() error = %v, want it distinguishable from revocation", err)
	}
}

func TestAuthVersion_UnsetReadsAsZero(t *testing.T) {
	store, _ := newTestStore(t)

	version, err := AuthVersion(context.Background(), store.client, 42)
	if err != nil {
		t.Fatalf("AuthVersion() error = %v", err)
	}
	if version != 0 {
		t.Errorf("AuthVersion() = %d, want 0", version)
	}
}

func TestBumpAuthVersion_CountsUpAndExpires(t *testing.T) {
	store, mr := newTestStore(t)

	for want := int64(1); want <= 3; want++ {
		got, err := BumpAuthVersion(context.Background(), store.client, 42, time.Hour)
		if err != nil {
			t.Fatalf("BumpAuthVersion() error = %v", err)
		}
		if got != want {
			t.Errorf("BumpAuthVersion() = %d, want %d", got, want)
		}
	}

	if ttl := mr.TTL(AuthVersionKey(42)); ttl != time.Hour {
		t.Errorf("TTL = %v, want %v", ttl, time.Hour)
	}

	version, err := AuthVersion(context.Background(), store.client, 42)
	if err != nil {
		t.Fatalf("AuthVersion() error = %v", err)
	}
	if version != 3 {
		t.Errorf("AuthVersion() = %d, want 3", version)
	}
}

func TestBumpAuthVersion_FailureLeavesNoKeyWithoutTTL(t *testing.T) {
	store, mr := newTestStore(t)
	mr.SetError("bump failed")

	if _, err := BumpAuthVersion(context.Background(), store.client, 42, time.Hour); err == nil {
		t.Fatal("BumpAuthVersion() error = nil, want the redis failure")
	}

	mr.SetError("")
	if mr.Exists(AuthVersionKey(42)) {
		t.Errorf("key %s survived a failed bump, so its TTL was never set", AuthVersionKey(42))
	}
}

func TestNewStore_NilClient(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when building a store with no client")
		}
	}()

	NewStore(nil)
}
