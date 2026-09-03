package sync

import (
	"context"
	"encoding/json"
	"seanime/internal/api/simkl"
	"seanime/internal/database/models"
	"seanime/internal/platforms/platform"
	"time"
)

// PendingSyncEnqueuer is the subset of *db.Database this package depends on, so tests can
// substitute a fake instead of a real database.
type PendingSyncEnqueuer interface {
	EnqueuePendingSync(item *models.PendingSync) error
}

// MirroringPlatform wraps a real platform.Platform, delegating every read method unchanged
// (via interface embedding) and intercepting only the mutating methods to: (1) always
// attempt the real AniList call and queue it for retry if it fails, and (2) always attempt
// a best-effort SIMKL mirror afterward, regardless of whether the AniList call succeeded.
type MirroringPlatform struct {
	platform.Platform
	simklClient  simkl.Client
	queue        PendingSyncEnqueuer
	profileID    string
	simklEnabled func() bool
}

func NewMirroringPlatform(inner platform.Platform, simklClient simkl.Client, queue PendingSyncEnqueuer, profileID string, simklEnabled func() bool) platform.Platform {
	return &MirroringPlatform{
		Platform:     inner,
		simklClient:  simklClient,
		queue:        queue,
		profileID:    profileID,
		simklEnabled: simklEnabled,
	}
}

type updateProgressPayload struct {
	MediaID       int  `json:"mediaId"`
	Progress      int  `json:"progress"`
	TotalEpisodes *int `json:"totalEpisodes,omitempty"`
}

func (m *MirroringPlatform) UpdateEntryProgress(ctx context.Context, mediaID int, progress int, totalEpisodes *int) error {
	anilistErr := m.Platform.UpdateEntryProgress(ctx, mediaID, progress, totalEpisodes)
	if anilistErr != nil {
		m.enqueue("anilist", "update_progress", updateProgressPayload{MediaID: mediaID, Progress: progress, TotalEpisodes: totalEpisodes})
	}

	if m.simklEnabled() {
		if simklErr := m.simklClient.MarkProgress(ctx, mediaID, progress); simklErr != nil {
			m.enqueue("simkl", "update_progress", updateProgressPayload{MediaID: mediaID, Progress: progress, TotalEpisodes: totalEpisodes})
		}
	}

	return anilistErr
}

// enqueue best-effort persists a pending retry row. If persisting itself fails there is
// nothing more this layer can do - it does not change what's returned to the caller, since
// the original AniList/SIMKL error (or lack thereof) is what the caller needs to see.
func (m *MirroringPlatform) enqueue(target, operation string, payload interface{}) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = m.queue.EnqueuePendingSync(&models.PendingSync{
		ProfileID:     m.profileID,
		Target:        target,
		Operation:     operation,
		Payload:       encoded,
		NextAttemptAt: time.Now().Add(time.Minute),
	})
}
