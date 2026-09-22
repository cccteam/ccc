// Package auth holds what the auths share: the request-scoped record of which auth a
// request came through. Each auth is its own package beneath this one (staff, members),
// owning its session manager, its permission store, and its roles file. A surface binds a
// request to an auth in the session group that authenticated it, and everything that
// answers for that request — permission checks, tenant visibility — answers from that
// auth's store.
package auth

import "context"

type ctxKey struct{}

// Bind records the auth a request came through: the name of the auth package whose
// session group authenticated it.
func Bind(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, ctxKey{}, name)
}

// Name returns the auth the request came through, or "" when no session group bound it,
// which every surface treats as its default auth.
func Name(ctx context.Context) string {
	name, _ := ctx.Value(ctxKey{}).(string)

	return name
}
