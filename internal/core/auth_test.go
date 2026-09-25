package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateTokenWithRememberRoundTrips guards the "remember me" flag surviving a full
// sign/parse round trip - renewAuthCookieIfNeeded relies on claims.Remember to pick the correct
// lifetime when reissuing a session, so a token minted with remember=true must still report it
// after ParseToken.
func TestGenerateTokenWithRememberRoundTrips(t *testing.T) {
	const secret = "test-jwt-secret"

	t.Run("remember=true", func(t *testing.T) {
		tokenString, err := GenerateTokenWithRemember(secret, "profile-1", false, "profile", time.Hour, true)
		require.NoError(t, err)

		claims, err := ParseToken(secret, tokenString)
		require.NoError(t, err)
		assert.True(t, claims.Remember)
		assert.Equal(t, "profile-1", claims.ProfileID)
	})

	t.Run("remember=false", func(t *testing.T) {
		tokenString, err := GenerateTokenWithRemember(secret, "profile-1", false, "profile", time.Hour, false)
		require.NoError(t, err)

		claims, err := ParseToken(secret, tokenString)
		require.NoError(t, err)
		assert.False(t, claims.Remember)
	})

	t.Run("GenerateToken defaults to remember=false", func(t *testing.T) {
		tokenString, err := GenerateToken(secret, "profile-1", false, "profile", time.Hour)
		require.NoError(t, err)

		claims, err := ParseToken(secret, tokenString)
		require.NoError(t, err)
		assert.False(t, claims.Remember)
	})
}
