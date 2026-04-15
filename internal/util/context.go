package util

import "context"

type contextKey string

const profileIDContextKey contextKey = "profileID"

// ContextWithProfileID returns a new context with the given profileID embedded.
func ContextWithProfileID(ctx context.Context, profileID string) context.Context {
	return context.WithValue(ctx, profileIDContextKey, profileID)
}

// ProfileIDFromContext extracts the profileID from the context, returning empty string if not set.
func ProfileIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(profileIDContextKey).(string); ok {
		return v
	}
	return ""
}
