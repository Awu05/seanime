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

// FallbackFetcher is the subset of simkl.Client FallbackPlatform needs for collection reads,
// kept separate from the write-only methods MirroringPlatform depends on.
type FallbackFetcher interface {
	simkl.Client
	GetAllItems(ctx context.Context) ([]simkl.AllItemsEntry, error)
}

// FallbackPlatform wraps platform.Platform (embedding it, so every read/write method not
// overridden below passes through unchanged) and redirects a fixed set of tracking operations to
// SIMKL only when the wrapped call actually fails AND AniList is known to be down
// (shared_platform.IsWorking, passed in as anilistHealthy so tests don't need global state).
// It never guesses proactively - a healthy AniList is byte-for-byte today's behavior.
//
// Writes redirected to SIMKL are also enqueued on the same PendingSync queue MirroringPlatform
// uses (target "anilist"), so Worker's existing anilistHealthy-gated delivery replays them once
// AniList recovers - no new delivery code needed.
//
// Install this wrapping OUTSIDE MirroringPlatform (raw -> MirroringPlatform -> FallbackPlatform):
// if AniList is down, there is no point letting MirroringPlatform attempt (and fail, and queue)
// the doomed AniList call before FallbackPlatform gets a turn.
type FallbackPlatform struct {
	platform.Platform
	simklClient    FallbackFetcher
	queue          PendingSyncEnqueuer
	profileID      string
	simklAvailable func() bool
	anilistHealthy func() bool
}

func NewFallbackPlatform(inner platform.Platform, simklClient FallbackFetcher, queue PendingSyncEnqueuer, profileID string, simklAvailable func() bool, anilistHealthy func() bool) platform.Platform {
	return &FallbackPlatform{
		Platform:       inner,
		simklClient:    simklClient,
		queue:          queue,
		profileID:      profileID,
		simklAvailable: simklAvailable,
		anilistHealthy: anilistHealthy,
	}
}

// canFallback reports whether this call is eligible to redirect to SIMKL: AniList must be known
// down AND this profile must have SIMKL connected+enabled. A profile without SIMKL just gets
// today's real error - there is nothing to fall back to.
func (f *FallbackPlatform) canFallback() bool {
	return !f.anilistHealthy() && f.simklAvailable()
}

func (f *FallbackPlatform) GetAnimeCollection(ctx context.Context, bypassCache bool) (*anilist.AnimeCollection, error) {
	collection, err := f.Platform.GetAnimeCollection(ctx, bypassCache)
	if err == nil || !f.canFallback() {
		return collection, err
	}
	entries, simklErr := f.simklClient.GetAllItems(ctx)
	if simklErr != nil {
		return nil, err // surface the original AniList error, not the SIMKL one - AniList is what the caller asked for
	}
	return BuildAnimeCollectionFromSimkl(entries), nil
}

// GetRawAnimeCollection has no "custom lists" concept on SIMKL - the fallback collection is
// identical to GetAnimeCollection's.
func (f *FallbackPlatform) GetRawAnimeCollection(ctx context.Context, bypassCache bool) (*anilist.AnimeCollection, error) {
	collection, err := f.Platform.GetRawAnimeCollection(ctx, bypassCache)
	if err == nil || !f.canFallback() {
		return collection, err
	}
	entries, simklErr := f.simklClient.GetAllItems(ctx)
	if simklErr != nil {
		return nil, err
	}
	return BuildAnimeCollectionFromSimkl(entries), nil
}

func (f *FallbackPlatform) UpdateEntryProgress(ctx context.Context, mediaID int, progress int, totalEpisodes *int) error {
	err := f.Platform.UpdateEntryProgress(ctx, mediaID, progress, totalEpisodes)
	if err == nil || !f.canFallback() {
		return err
	}
	if simklErr := f.simklClient.MarkProgress(ctx, mediaID, progress); simklErr != nil {
		return err
	}
	f.enqueueAnilistCatchUp(OpUpdateProgress, UpdateProgressPayload{MediaID: mediaID, Progress: progress, TotalEpisodes: totalEpisodes})
	return nil
}

func (f *FallbackPlatform) UpdateEntry(ctx context.Context, mediaID int, status *anilist.MediaListStatus, scoreRaw *int, progress *int, startedAt *anilist.FuzzyDateInput, completedAt *anilist.FuzzyDateInput) error {
	err := f.Platform.UpdateEntry(ctx, mediaID, status, scoreRaw, progress, startedAt, completedAt)
	if err == nil || !f.canFallback() {
		return err
	}

	var simklFailed bool
	if status != nil {
		if simklErr := f.simklClient.AddToList(ctx, mediaID, MapAnilistStatusToSimkl(*status)); simklErr != nil {
			simklFailed = true
		}
	}
	if scoreRaw != nil {
		rating, shouldRemove := MapAnilistScoreToSimklRating(*scoreRaw)
		var simklErr error
		if shouldRemove {
			simklErr = f.simklClient.RemoveRating(ctx, mediaID)
		} else {
			simklErr = f.simklClient.SetRating(ctx, mediaID, rating)
		}
		if simklErr != nil {
			simklFailed = true
		}
	}
	if simklFailed {
		return err
	}

	f.enqueueAnilistCatchUp(OpUpdateEntry, UpdateEntryPayload{MediaID: mediaID, Status: status, ScoreRaw: scoreRaw, Progress: progress, StartedAt: startedAt, CompletedAt: completedAt})
	return nil
}

func (f *FallbackPlatform) DeleteEntry(ctx context.Context, mediaID int, entryID int) error {
	err := f.Platform.DeleteEntry(ctx, mediaID, entryID)
	if err == nil || !f.canFallback() {
		return err
	}
	if simklErr := f.simklClient.RemoveEntry(ctx, mediaID); simklErr != nil {
		return err
	}
	f.enqueueAnilistCatchUp(OpDeleteEntry, DeleteEntryPayload{MediaID: mediaID, EntryID: entryID})
	return nil
}

// enqueueAnilistCatchUp persists a PendingSync{Target: "anilist"} row so Worker replays this
// write into AniList once IsWorking flips back to true. Best-effort: if persisting itself fails
// there's nothing more this layer can do, matching MirroringPlatform.enqueue's own contract.
func (f *FallbackPlatform) enqueueAnilistCatchUp(operation string, payload interface{}) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = f.queue.EnqueuePendingSync(&models.PendingSync{
		ProfileID:     f.profileID,
		Target:        TargetAnilist,
		Operation:     operation,
		Payload:       encoded,
		NextAttemptAt: time.Now().Add(initialRetryDelay),
	})
}
