package core

import (
	"seanime/internal/directstream"
	"seanime/internal/library/playbackmanager"
	"seanime/internal/nativeplayer"
	"seanime/internal/torrentstream"
	"seanime/internal/videocore"
	"sync"
	"time"
)

type ProfileStreamSession struct {
	ProfileID           string
	LastActive          time.Time
	VideoCore           *videocore.VideoCore
	NativePlayer        *nativeplayer.NativePlayer
	PlaybackManager     *playbackmanager.PlaybackManager
	DirectStreamManager *directstream.Manager
	TorrentStream       *torrentstream.Repository
	// StreamToken returns a token letting an external player fetch this profile's streams
	// (see StreamScope), or "" when none is needed.
	StreamToken func() string
}

type StreamSessionManager struct {
	sessions          map[string]*ProfileStreamSession
	mu                sync.Mutex
	cleanupTicker     *time.Ticker
	cleanupDone       chan struct{}
	inactivityTimeout time.Duration
	keepAlive         func(*ProfileStreamSession) bool // guarded by mu; see SetKeepAlive
}

func NewStreamSessionManager(inactivityTimeout time.Duration) *StreamSessionManager {
	sm := &StreamSessionManager{
		sessions:          make(map[string]*ProfileStreamSession),
		inactivityTimeout: inactivityTimeout,
		cleanupTicker:     time.NewTicker(5 * time.Minute),
		cleanupDone:       make(chan struct{}),
	}
	go sm.cleanupLoop()
	return sm
}

// GetOrCreateSession returns the profile's session, creating one via factory if absent.
// The second return value is true when a new session was created, enabling callers to
// trigger one-time post-creation work (e.g., seeding collection from an I/O-blocking source)
// outside the lock.
func (sm *StreamSessionManager) GetOrCreateSession(profileID string, factory func(string) *ProfileStreamSession) (*ProfileStreamSession, bool) {
	if profileID == "" {
		profileID = DefaultProfileID
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[profileID]
	if !exists {
		session = factory(profileID)
		session.ProfileID = profileID
		sm.sessions[profileID] = session
		return session, true
	}
	session.LastActive = time.Now()
	return session, false
}

// PeekSession returns the profile's session without creating one if absent, unlike
// GetOrCreateSession. Used by admin actions that must never spin up a session for a
// profile that doesn't have one.
func (sm *StreamSessionManager) PeekSession(profileID string) (*ProfileStreamSession, bool) {
	if profileID == "" {
		profileID = DefaultProfileID
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[profileID]
	return session, exists
}

// WithSessionsLocked runs fn while holding the write lock, passing in a snapshot of
// the current sessions. Use this to atomically update external state AND propagate to
// existing sessions, serializing against concurrent session creation.
// fn must not call back into StreamSessionManager methods (deadlock) and must not
// perform blocking I/O (blocks all session creation and settings refreshes for its duration).
func (sm *StreamSessionManager) WithSessionsLocked(fn func(sessions []*ProfileStreamSession)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sessions := make([]*ProfileStreamSession, 0, len(sm.sessions))
	for _, s := range sm.sessions {
		sessions = append(sessions, s)
	}
	fn(sessions)
}

// EvictSession removes and shuts down a profile's session. Called when the
// profile's AniList identity changes (login/logout) so the next request
// rebuilds the session with a platform carrying the current token.
// Safe to call when no session exists.
func (sm *StreamSessionManager) EvictSession(profileID string) {
	if profileID == "" {
		profileID = DefaultProfileID
	}
	sm.mu.Lock()
	session, ok := sm.sessions[profileID]
	if ok {
		delete(sm.sessions, profileID)
	}
	sm.mu.Unlock()
	if ok {
		session.Shutdown()
	}
}

// SetKeepAlive exempts sessions for which fn returns true from idle eviction, e.g. one a watch
// party is playing through without sending stream requests. fn runs under the manager lock, so it
// must not call back into StreamSessionManager.
func (sm *StreamSessionManager) SetKeepAlive(fn func(*ProfileStreamSession) bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.keepAlive = fn
}

// evictIdle removes and returns sessions idle past the timeout; the caller shuts them down
// outside the lock so component cleanup can't block other sessions.
func (sm *StreamSessionManager) evictIdle(now time.Time) []*ProfileStreamSession {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	var evicted []*ProfileStreamSession
	for id, session := range sm.sessions {
		if now.Sub(session.LastActive) <= sm.inactivityTimeout || (sm.keepAlive != nil && sm.keepAlive(session)) {
			continue
		}
		evicted = append(evicted, session)
		delete(sm.sessions, id)
	}
	return evicted
}

func (sm *StreamSessionManager) cleanupLoop() {
	for {
		select {
		case <-sm.cleanupTicker.C:
			for _, session := range sm.evictIdle(time.Now()) {
				session.Shutdown()
			}
		case <-sm.cleanupDone:
			return
		}
	}
}

func (sm *StreamSessionManager) Stop() {
	sm.cleanupTicker.Stop()
	close(sm.cleanupDone)

	// Shutdown all active sessions outside the lock to avoid deadlock.
	sm.mu.Lock()
	sessions := make([]*ProfileStreamSession, 0, len(sm.sessions))
	for id, s := range sm.sessions {
		sessions = append(sessions, s)
		delete(sm.sessions, id)
	}
	sm.mu.Unlock()
	for _, session := range sessions {
		session.Shutdown()
	}
}
