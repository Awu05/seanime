package sync

import (
	"context"
	"encoding/json"
	"seanime/internal/api/anilist"
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

type updateEntryPayload struct {
	MediaID     int                      `json:"mediaId"`
	Status      *anilist.MediaListStatus `json:"status,omitempty"`
	ScoreRaw    *int                     `json:"scoreRaw,omitempty"`
	Progress    *int                     `json:"progress,omitempty"`
	StartedAt   *anilist.FuzzyDateInput  `json:"startedAt,omitempty"`
	CompletedAt *anilist.FuzzyDateInput  `json:"completedAt,omitempty"`
}

func (m *MirroringPlatform) UpdateEntry(ctx context.Context, mediaID int, status *anilist.MediaListStatus, scoreRaw *int, progress *int, startedAt *anilist.FuzzyDateInput, completedAt *anilist.FuzzyDateInput) error {
	anilistErr := m.Platform.UpdateEntry(ctx, mediaID, status, scoreRaw, progress, startedAt, completedAt)
	payload := updateEntryPayload{MediaID: mediaID, Status: status, ScoreRaw: scoreRaw, Progress: progress, StartedAt: startedAt, CompletedAt: completedAt}
	if anilistErr != nil {
		m.enqueue("anilist", "update_entry", payload)
	}

	if m.simklEnabled() {
		var simklFailed bool
		if status != nil {
			if err := m.simklClient.AddToList(ctx, mediaID, MapAnilistStatusToSimkl(*status)); err != nil {
				simklFailed = true
			}
		}
		if scoreRaw != nil {
			rating, shouldRemove := MapAnilistScoreToSimklRating(*scoreRaw)
			var err error
			if shouldRemove {
				err = m.simklClient.RemoveRating(ctx, mediaID)
			} else {
				err = m.simklClient.SetRating(ctx, mediaID, rating)
			}
			if err != nil {
				simklFailed = true
			}
		}
		if simklFailed {
			m.enqueue("simkl", "update_entry", payload)
		}
	}

	return anilistErr
}

type updateRepeatPayload struct {
	MediaID int `json:"mediaId"`
	Repeat  int `json:"repeat"`
}

func (m *MirroringPlatform) UpdateEntryRepeat(ctx context.Context, mediaID int, repeat int) error {
	anilistErr := m.Platform.UpdateEntryRepeat(ctx, mediaID, repeat)
	if anilistErr != nil {
		m.enqueue("anilist", "update_repeat", updateRepeatPayload{MediaID: mediaID, Repeat: repeat})
	}
	// SIMKL has no direct "repeat count" concept exposed by the sync endpoints used here;
	// rewatch tracking on SIMKL's side is out of scope per the design spec.
	return anilistErr
}

type deleteEntryPayload struct {
	MediaID int `json:"mediaId"`
	EntryID int `json:"entryId"`
}

func (m *MirroringPlatform) DeleteEntry(ctx context.Context, mediaID int, entryID int) error {
	anilistErr := m.Platform.DeleteEntry(ctx, mediaID, entryID)
	if anilistErr != nil {
		m.enqueue("anilist", "delete_entry", deleteEntryPayload{MediaID: mediaID, EntryID: entryID})
	}

	if m.simklEnabled() {
		if err := m.simklClient.RemoveEntry(ctx, mediaID); err != nil {
			m.enqueue("simkl", "delete_entry", deleteEntryPayload{MediaID: mediaID, EntryID: entryID})
		}
	}

	return anilistErr
}

type addToCollectionPayload struct {
	MediaIDs []int `json:"mediaIds"`
}

func (m *MirroringPlatform) AddMediaToCollection(ctx context.Context, mIds []int) error {
	anilistErr := m.Platform.AddMediaToCollection(ctx, mIds)
	if anilistErr != nil {
		m.enqueue("anilist", "add_to_collection", addToCollectionPayload{MediaIDs: mIds})
	}

	if m.simklEnabled() {
		var simklFailed bool
		for _, id := range mIds {
			if err := m.simklClient.AddToList(ctx, id, "plantowatch"); err != nil {
				simklFailed = true
			}
		}
		if simklFailed {
			m.enqueue("simkl", "add_to_collection", addToCollectionPayload{MediaIDs: mIds})
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
