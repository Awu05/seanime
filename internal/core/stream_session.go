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
	mu                sync.RWMutex
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

func (sm *StreamSessionManager) GetSession(profileID string) *ProfileStreamSession {
	if profileID == "" {
		profileID = "_default"
	}
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sessions[profileID]
}

func (sm *StreamSessionManager) RemoveSession(profileID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.sessions, profileID)
}

func (sm *StreamSessionManager) GetActiveSessions() []*ProfileStreamSession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	sessions := make([]*ProfileStreamSession, 0, len(sm.sessions))
	for _, s := range sm.sessions {
		sessions = append(sessions, s)
	}
	return sessions
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
