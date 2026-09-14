package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGetOrCreateSessionSetsProfileID(t *testing.T) {
	sm := NewStreamSessionManager(5 * time.Minute)
	t.Cleanup(sm.Stop)

	factory := func(profileID string) *ProfileStreamSession {
		return &ProfileStreamSession{}
	}

	session, created := sm.GetOrCreateSession("profile-1", factory)
	require.True(t, created)
	require.Equal(t, "profile-1", session.ProfileID)

	// Second call for the same profile must not create a new session, and must
	// keep the ProfileID set.
	session2, created2 := sm.GetOrCreateSession("profile-1", factory)
	require.False(t, created2)
	require.Same(t, session, session2)
	require.Equal(t, "profile-1", session2.ProfileID)
}

func TestPeekSessionDoesNotCreate(t *testing.T) {
	sm := NewStreamSessionManager(5 * time.Minute)
	t.Cleanup(sm.Stop)

	session, found := sm.PeekSession("no-such-profile")
	require.False(t, found)
	require.Nil(t, session)

	factory := func(profileID string) *ProfileStreamSession {
		return &ProfileStreamSession{}
	}
	created, _ := sm.GetOrCreateSession("profile-1", factory)

	peeked, found := sm.PeekSession("profile-1")
	require.True(t, found)
	require.Same(t, created, peeked)
}
