package sync

import (
	"context"
	"encoding/json"
	"seanime/internal/api/anilist"
	"seanime/internal/api/simkl"
	"seanime/internal/customsource"
	"seanime/internal/database/models"
	"seanime/internal/platforms/platform"
	"time"
)

// FallbackPlatform wraps platform.Platform (embedding it, so every read/write method not
// overridden below passes through unchanged) and redirects a fixed set of tracking operations to
// SIMKL only when AniList is known to be down (shared_platform.IsWorking, passed in as
// anilistHealthy so tests don't need global state). It never guesses proactively - a healthy
// AniList is byte-for-byte today's behavior.
//
// Writes redirected to SIMKL are also enqueued on the same PendingSync queue MirroringPlatform
// uses (target "anilist"), so Worker's existing anilistHealthy-gated delivery replays them once
// AniList recovers - no new delivery code needed.
//
// Install this wrapping OUTSIDE MirroringPlatform (raw -> MirroringPlatform -> FallbackPlatform).
// Note that MirroringPlatform still gets a turn first on any write that falls through to
// f.Platform below (FallbackPlatform is reactive - see the write methods' doc comments for why
// they check canFallback() and attempt SIMKL BEFORE ever calling f.Platform, precisely to avoid
// a double SIMKL write / double AniList-catch-up-enqueue against MirroringPlatform's own
// always-mirror-regardless-of-success logic underneath).
type FallbackPlatform struct {
	platform.Platform
	simklClient    simkl.Client
	queue          PendingSyncEnqueuer
	profileID      string
	simklAvailable func() bool
	anilistHealthy func() bool
}

func NewFallbackPlatform(inner platform.Platform, simklClient simkl.Client, queue PendingSyncEnqueuer, profileID string, simklAvailable func() bool, anilistHealthy func() bool) platform.Platform {
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

// GetAnimeCollection calls the wrapped platform FIRST (unlike the write methods below): this
// preserves AniList's cache-preferring read behavior, only genuinely falling back to SIMKL when
// there's no usable cache AND the underlying call fails.
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

// UpdateEntryProgress checks canFallback() (plus the progress>0 and manga guards) and attempts
// the full SIMKL-write-plus-enqueue chain BEFORE ever calling f.Platform. This ordering matters:
// f.Platform is MirroringPlatform, which already does its own "always mirror to SIMKL, always
// enqueue an anilist catch-up row on failure" on every failed call - calling it first and THEN
// falling back would double-write to SIMKL and double-enqueue the same anilist retry row. Only
// when the SIMKL-fallback chain doesn't fully complete (SIMKL write fails, or the durable
// catch-up enqueue fails) does this fall through to f.Platform for its normal behavior.
//
// progress <= 0 is skipped: MarkProgress(0) builds an empty episode list, which is
// byte-identical to RemoveEntry's request body and would DELETE the SIMKL backup entry - a
// progress reset must be a no-op on the backup, never a deletion (same guard MirroringPlatform
// already applies for the same reason).
//
// customsource.IsExtensionId(mediaID) media is skipped too: those are synthetic IDs for
// custom-source entries, not real AniList IDs - shared_platform routes them to a local
// custom-source manager instead of AniList entirely (shared.go), so they never had an AniList
// call to fall back from, and writing one to SIMKL would create a bogus entry under a fake ID.
func (f *FallbackPlatform) UpdateEntryProgress(ctx context.Context, mediaID int, progress int, totalEpisodes *int) error {
	if f.canFallback() && progress > 0 && !isMangaMedia(ctx) && !customsource.IsExtensionId(mediaID) {
		if simklErr := f.simklClient.MarkProgress(ctx, mediaID, progress); simklErr == nil {
			if enqErr := f.enqueueAnilistCatchUp(OpUpdateProgress, UpdateProgressPayload{MediaID: mediaID, Progress: progress, TotalEpisodes: totalEpisodes}); enqErr == nil {
				return nil
			}
		}
	}
	return f.Platform.UpdateEntryProgress(ctx, mediaID, progress, totalEpisodes)
}

// UpdateEntry - see UpdateEntryProgress's doc comment for why canFallback() and the SIMKL
// attempt happen before f.Platform is ever called, and for why manga and custom-source media
// are excluded.
func (f *FallbackPlatform) UpdateEntry(ctx context.Context, mediaID int, status *anilist.MediaListStatus, scoreRaw *int, progress *int, startedAt *anilist.FuzzyDateInput, completedAt *anilist.FuzzyDateInput) error {
	if f.canFallback() && !isMangaMedia(ctx) && !customsource.IsExtensionId(mediaID) {
		var simklFailed bool
		if status != nil {
			if err := f.simklClient.AddToList(ctx, mediaID, MapAnilistStatusToSimkl(*status)); err != nil {
				simklFailed = true
			}
		}
		if scoreRaw != nil {
			rating, shouldRemove := MapAnilistScoreToSimklRating(*scoreRaw)
			var err error
			if shouldRemove {
				err = f.simklClient.RemoveRating(ctx, mediaID)
			} else {
				err = f.simklClient.SetRating(ctx, mediaID, rating)
			}
			if err != nil {
				simklFailed = true
			}
		}
		if !simklFailed {
			if enqErr := f.enqueueAnilistCatchUp(OpUpdateEntry, UpdateEntryPayload{MediaID: mediaID, Status: status, ScoreRaw: scoreRaw, Progress: progress, StartedAt: startedAt, CompletedAt: completedAt}); enqErr == nil {
				return nil
			}
		}
	}
	return f.Platform.UpdateEntry(ctx, mediaID, status, scoreRaw, progress, startedAt, completedAt)
}

// DeleteEntry - see UpdateEntryProgress's doc comment for why canFallback() and the SIMKL
// attempt happen before f.Platform is ever called, and for why manga and custom-source media
// are excluded.
func (f *FallbackPlatform) DeleteEntry(ctx context.Context, mediaID int, entryID int) error {
	if f.canFallback() && !isMangaMedia(ctx) && !customsource.IsExtensionId(mediaID) {
		if simklErr := f.simklClient.RemoveEntry(ctx, mediaID); simklErr == nil {
			if enqErr := f.enqueueAnilistCatchUp(OpDeleteEntry, DeleteEntryPayload{MediaID: mediaID, EntryID: entryID}); enqErr == nil {
				return nil
			}
		}
	}
	return f.Platform.DeleteEntry(ctx, mediaID, entryID)
}

// enqueueAnilistCatchUp persists a PendingSync{Target: "anilist"} row so Worker replays this
// write into AniList once IsWorking flips back to true. Returns an error if persisting failed -
// callers must NOT report success when the write only "succeeded" on SIMKL with no durable
// record to replay it into AniList, since SIMKL is a fallback, not a permanent second home.
//
// Known, accepted residual edge case: if the SIMKL write above succeeds but this enqueue fails,
// the caller falls through to f.Platform (MirroringPlatform), which will attempt its own SIMKL
// mirror too - a second, now-redundant SIMKL write. This only happens when a live network write
// is immediately followed by a local DB write failure, is harmless for progress/status
// (idempotent), and is not worth engineering around further.
func (f *FallbackPlatform) enqueueAnilistCatchUp(operation string, payload interface{}) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return f.queue.EnqueuePendingSync(&models.PendingSync{
		ProfileID:     f.profileID,
		Target:        TargetAnilist,
		Operation:     operation,
		Payload:       encoded,
		NextAttemptAt: time.Now().Add(initialRetryDelay),
	})
}
