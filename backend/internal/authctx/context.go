// Package authctx carries the caller's identity through a request's context,
// independent of how it was established — a real session, the DevAuth stub, or
// an anonymous-draft token. Every handler downstream reads it through UserID
// (authenticated) or AnonToken (anonymous); the auth middleware sets exactly
// one and fails closed when it can set neither.
//
// @spec ACCT-002
package authctx

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

type contextKey int

const (
	userIDKey contextKey = iota
	anonTokenKey
)

// WithUserID returns a copy of ctx carrying the authenticated user's ID.
func WithUserID(ctx context.Context, userID pgtype.UUID) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// UserID returns the authenticated user's ID and whether one was present. The
// auth middleware fails closed, so a handler that runs at all has either a user
// ID or an anon token.
func UserID(ctx context.Context) (pgtype.UUID, bool) {
	id, ok := ctx.Value(userIDKey).(pgtype.UUID)
	return id, ok && id.Valid
}

// WithAnonToken returns a copy of ctx carrying an anonymous-draft token. Used
// for a caller with no session who presented an X-Anon-Token header (ACCT-020).
func WithAnonToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, anonTokenKey, token)
}

// AnonToken returns the anonymous-draft token and whether a non-empty one was
// present.
func AnonToken(ctx context.Context) (string, bool) {
	tok, ok := ctx.Value(anonTokenKey).(string)
	return tok, ok && tok != ""
}
