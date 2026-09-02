// Package authctx carries the authenticated user's ID through a request's
// context, independent of how that user was authenticated — today the DevAuth
// stub, from M3 real sessions. Every handler downstream calls UserID(ctx)
// either way.
//
// @spec ACCT-002
package authctx

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

type contextKey int

const userIDKey contextKey = iota

// WithUserID returns a copy of ctx carrying the authenticated user's ID.
func WithUserID(ctx context.Context, userID pgtype.UUID) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// UserID returns the authenticated user's ID and whether one was present. The
// auth middleware fails closed, so a handler that runs at all can trust the ID
// is set and need not check the bool.
func UserID(ctx context.Context) (pgtype.UUID, bool) {
	id, ok := ctx.Value(userIDKey).(pgtype.UUID)
	return id, ok
}
