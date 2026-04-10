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

func (sm *StreamSessionManager) GetOrCreateSession(profileID string, factory func(string) *ProfileStreamSession) *ProfileStreamSession {
	if profileID == "" {
		profileID = "_default"
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[profileID]
	if !exists {
		session = factory(profileID)
		sm.sessions[profileID] = session
	} else {
		session.LastActive = time.Now()
	}
	return session
}

// WithSessionsLocked runs fn while holding the write lock, passing in a snapshot of
// the current sessions. Use this to atomically update external state AND propagate to
// existing sessions, serializing against concurrent session creation.
// fn must not call back into StreamSessionManager methods (deadlock).
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
			sm.mu.Lock()
			now := time.Now()
			for id, session := range sm.sessions {
				if now.Sub(session.LastActive) > sm.inactivityTimeout {
					delete(sm.sessions, id)
				}
			}
			sm.mu.Unlock()
		case <-sm.cleanupDone:
			return
		}
	}
}

func (sm *StreamSessionManager) Stop() {
	sm.cleanupTicker.Stop()
	close(sm.cleanupDone)
}
