package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestEvictIdleSparesKeptAliveSessions guards a watch party whose host goes a long time without
// a stream request: evicting the host's session mid-party left the party driving shut-down players.
func TestEvictIdleSparesKeptAliveSessions(t *testing.T) {
	long := time.Now().Add(-3 * time.Hour)
	sm := &StreamSessionManager{
		inactivityTimeout: time.Hour,
		sessions: map[string]*ProfileStreamSession{
			"idle":  {ProfileID: "idle", LastActive: long},
			"party": {ProfileID: "party", LastActive: long},
		},
	}
	sm.SetKeepAlive(func(s *ProfileStreamSession) bool { return s.ProfileID == "party" })

	evicted := sm.evictIdle(time.Now())

	require.Len(t, evicted, 1)
	require.Equal(t, "idle", evicted[0].ProfileID)
	_, kept := sm.PeekSession("party")
	require.True(t, kept)
}

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
