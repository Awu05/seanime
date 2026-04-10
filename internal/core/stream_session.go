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
}

type StreamSessionManager struct {
	sessions          map[string]*ProfileStreamSession
	mu                sync.Mutex
	cleanupTicker     *time.Ticker
	cleanupDone       chan struct{}
	inactivityTimeout time.Duration
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
		profileID = "_default"
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[profileID]
	if !exists {
		session = factory(profileID)
		sm.sessions[profileID] = session
		return session, true
	}
	session.LastActive = time.Now()
	return session, false
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

func (sm *StreamSessionManager) cleanupLoop() {
	for {
		select {
		case <-sm.cleanupTicker.C:
			// Collect evicted sessions under the lock, then run Shutdown outside
			// so component cleanup cannot block other sessions or deadlock on the manager lock.
			var evicted []*ProfileStreamSession
			sm.mu.Lock()
			now := time.Now()
			for id, session := range sm.sessions {
				if now.Sub(session.LastActive) > sm.inactivityTimeout {
					evicted = append(evicted, session)
					delete(sm.sessions, id)
				}
			}
			sm.mu.Unlock()
			for _, session := range evicted {
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
