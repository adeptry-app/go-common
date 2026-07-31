package database

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// actorType names the kind of identity a call acts under, matching the
// audit.actor_type enum.
type actorType string

const (
	actorUser      actorType = "user"
	actorAnonymous actorType = "anonymous"
)

// AuthContext is the audit.set_context() payload: the typed actor applied
// before every audited SQL function call. The fields are unexported and the
// constructors below are the only way in, so no caller can assemble an
// identity by hand; the zero value names no actor and the database rejects it.
type AuthContext struct {
	actor     actorType
	userID    int64
	clientIP  string
	userAgent string
}

// UserActor attributes the call to an authenticated user. There is no username
// argument: the database reads the name from the id, so the two cannot disagree.
func UserActor(userID int64, clientIP, userAgent string) AuthContext {
	return AuthContext{actor: actorUser, userID: userID, clientIP: clientIP, userAgent: userAgent}
}

// AnonymousActor attributes the call to nobody, for the routes served without a
// session.
func AnonymousActor() AuthContext {
	return AuthContext{actor: actorAnonymous}
}

// UserID is 0 for any actor that is not a user.
func (a AuthContext) UserID() int64 { return a.userID }

// userIDArg stays NULL for a non-user actor, which set_context requires.
func (a AuthContext) userIDArg() *int64 {
	if a.actor != actorUser {
		return nil
	}
	return &a.userID
}

// CallInto runs `SELECT audit.set_context(...)` and the given single-row
// query in one pgx batch, scanning the result into dest. A batch executes in
// one implicit transaction, so the transaction-local audit settings cover the
// query and an error in either statement aborts both - the semantics of an
// explicit BEGIN/COMMIT exchange in one network round trip.
func CallInto(ctx context.Context, pool *pgxpool.Pool, auth AuthContext, dest any, query string, args ...any) error {
	b := &pgx.Batch{}
	b.Queue("SELECT audit.set_context($1::audit.actor_type, $2, $3, $4)",
		string(auth.actor), auth.userIDArg(), auth.clientIP, auth.userAgent)
	b.Queue(query, args...)

	br := pool.SendBatch(ctx, b)
	defer func() { _ = br.Close() }()

	if _, err := br.Exec(); err != nil {
		return fmt.Errorf("set audit context: %w", err)
	}
	if err := br.QueryRow().Scan(dest); err != nil {
		return err
	}
	return br.Close()
}

// CallJSON runs an audited query returning JSONB (the SELECT schema.fn(...)
// convention). SQL NULL scans to a nil RawMessage.
func CallJSON(ctx context.Context, pool *pgxpool.Pool, auth AuthContext, query string, args ...any) (json.RawMessage, error) {
	var result json.RawMessage
	err := CallInto(ctx, pool, auth, &result, query, args...)
	return result, err
}

// CallBool runs an audited query returning a boolean (delete functions).
// A SQL NULL result fails the scan.
func CallBool(ctx context.Context, pool *pgxpool.Pool, auth AuthContext, query string, args ...any) (bool, error) {
	var result bool
	err := CallInto(ctx, pool, auth, &result, query, args...)
	return result, err
}

// CallDiscard runs an audited query and discards its scalar result.
func CallDiscard(ctx context.Context, pool *pgxpool.Pool, auth AuthContext, query string, args ...any) error {
	var discard any
	return CallInto(ctx, pool, auth, &discard, query, args...)
}
