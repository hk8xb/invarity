// Package auth provides authentication and authorization primitives.
package auth

import (
	"context"
)

// ActorType represents the type of authenticated actor.
type ActorType string

const (
	ActorTypeUser  ActorType = "user"
	ActorTypeAgent ActorType = "agent"
)

// AuthContext holds authentication information for the current request.
type AuthContext struct {
	ActorType ActorType
	UserID    string // Cognito sub claim
	Email     string // From Cognito email claim (if present)
}

// contextKey is a private type for context keys to avoid collisions.
type contextKey int

const authContextKey contextKey = iota

// WithAuthContext adds authentication context to a context.
func WithAuthContext(ctx context.Context, auth *AuthContext) context.Context {
	return context.WithValue(ctx, authContextKey, auth)
}

// GetAuthContext retrieves authentication context from a context.
// Returns nil if no auth context is present.
func GetAuthContext(ctx context.Context) *AuthContext {
	auth, ok := ctx.Value(authContextKey).(*AuthContext)
	if !ok {
		return nil
	}
	return auth
}

// RequireAuthContext retrieves authentication context or returns false.
func RequireAuthContext(ctx context.Context) (*AuthContext, bool) {
	auth := GetAuthContext(ctx)
	return auth, auth != nil
}
