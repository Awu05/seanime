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

// Target and operation names, shared by MirroringPlatform's enqueue calls, the Worker's
// delivery switches, and the SIMKL "sync now" seed - kept as named constants (rather than
// repeated string literals across three files) so a typo is a compile error, not a row that
// silently falls into the switch's default case and gets treated as delivered.
const (
	TargetAnilist = "anilist"
	TargetSimkl   = "simkl"

	OpUpdateProgress  = "update_progress"
	OpUpdateEntry     = "update_entry"
	OpUpdateRepeat    = "update_repeat"
	OpDeleteEntry     = "delete_entry"
	OpAddToCollection = "add_to_collection"
)

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

// RawPlatform returns the unwrapped platform underneath any MirroringPlatform/FallbackPlatform
// layers, or p unchanged if it isn't wrapped. The retry Worker MUST deliver AniList rows through
// this: replaying a row through either wrapper would re-run interception, enqueueing a fresh
// duplicate row on every failed attempt (unbounded queue growth) and re-mirroring to SIMKL on
// every success. Loops rather than checking a single type since FallbackPlatform wraps outside
// MirroringPlatform (raw -> MirroringPlatform -> FallbackPlatform) - a single type-assertion
// would strip only the outer layer and leave the row replayed through MirroringPlatform anyway.
func RawPlatform(p platform.Platform) platform.Platform {
	for {
		switch v := p.(type) {
		case *MirroringPlatform:
			p = v.Platform
		case *FallbackPlatform:
			p = v.Platform
		default:
			return p
		}
	}
}

type mangaMediaContextKey struct{}

// WithMangaMedia marks ctx as a manga list mutation. platform.Platform's mutating methods
// (UpdateEntry, UpdateEntryProgress, UpdateEntryRepeat, DeleteEntry, AddMediaToCollection) take
// no media-type argument and are shared by both anime and manga callers - e.g.
// HandleEditAnilistListEntry and HandleDeleteAnilistListEntry handle both from one endpoint,
// keyed only by mediaID. Without this marker MirroringPlatform can't tell manga mutations from
// anime ones, and mirrors them to SIMKL's anime-only endpoints anyway - creating bogus "anime"
// entries in the user's real SIMKL account for what are actually manga list changes.
func WithMangaMedia(ctx context.Context) context.Context {
	return context.WithValue(ctx, mangaMediaContextKey{}, true)
}

func isMangaMedia(ctx context.Context) bool {
	v, _ := ctx.Value(mangaMediaContextKey{}).(bool)
	return v
}

type UpdateProgressPayload struct {
	MediaID       int  `json:"mediaId"`
	Progress      int  `json:"progress"`
	TotalEpisodes *int `json:"totalEpisodes,omitempty"`
}

func (m *MirroringPlatform) UpdateEntryProgress(ctx context.Context, mediaID int, progress int, totalEpisodes *int) error {
	anilistErr := m.Platform.UpdateEntryProgress(ctx, mediaID, progress, totalEpisodes)
	if anilistErr != nil {
		m.enqueue(TargetAnilist, OpUpdateProgress, UpdateProgressPayload{MediaID: mediaID, Progress: progress, TotalEpisodes: totalEpisodes})
	}

	// progress <= 0 is skipped on the SIMKL side: MarkProgress(0) builds an empty episode list,
	// which is byte-identical to RemoveEntry's request body and would DELETE the backup entry.
	// A progress reset must be a no-op on the backup, never a deletion.
	if progress > 0 && m.simklEnabled() && !isMangaMedia(ctx) {
		if simklErr := m.simklClient.MarkProgress(ctx, mediaID, progress); simklErr != nil {
			m.enqueue(TargetSimkl, OpUpdateProgress, UpdateProgressPayload{MediaID: mediaID, Progress: progress, TotalEpisodes: totalEpisodes})
		}
	}

	return anilistErr
}

type UpdateEntryPayload struct {
	MediaID     int                      `json:"mediaId"`
	Status      *anilist.MediaListStatus `json:"status,omitempty"`
	ScoreRaw    *int                     `json:"scoreRaw,omitempty"`
	Progress    *int                     `json:"progress,omitempty"`
	StartedAt   *anilist.FuzzyDateInput  `json:"startedAt,omitempty"`
	CompletedAt *anilist.FuzzyDateInput  `json:"completedAt,omitempty"`
}

func (m *MirroringPlatform) UpdateEntry(ctx context.Context, mediaID int, status *anilist.MediaListStatus, scoreRaw *int, progress *int, startedAt *anilist.FuzzyDateInput, completedAt *anilist.FuzzyDateInput) error {
	anilistErr := m.Platform.UpdateEntry(ctx, mediaID, status, scoreRaw, progress, startedAt, completedAt)
	payload := UpdateEntryPayload{MediaID: mediaID, Status: status, ScoreRaw: scoreRaw, Progress: progress, StartedAt: startedAt, CompletedAt: completedAt}
	if anilistErr != nil {
		m.enqueue(TargetAnilist, OpUpdateEntry, payload)
	}

	if m.simklEnabled() && !isMangaMedia(ctx) {
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
			m.enqueue(TargetSimkl, OpUpdateEntry, payload)
		}
	}

	return anilistErr
}

type UpdateRepeatPayload struct {
	MediaID int `json:"mediaId"`
	Repeat  int `json:"repeat"`
}

func (m *MirroringPlatform) UpdateEntryRepeat(ctx context.Context, mediaID int, repeat int) error {
	anilistErr := m.Platform.UpdateEntryRepeat(ctx, mediaID, repeat)
	if anilistErr != nil {
		m.enqueue(TargetAnilist, OpUpdateRepeat, UpdateRepeatPayload{MediaID: mediaID, Repeat: repeat})
	}
	// SIMKL has no direct "repeat count" concept exposed by the sync endpoints used here;
	// rewatch tracking on SIMKL's side is out of scope per the design spec.
	return anilistErr
}

type DeleteEntryPayload struct {
	MediaID int `json:"mediaId"`
	EntryID int `json:"entryId"`
}

func (m *MirroringPlatform) DeleteEntry(ctx context.Context, mediaID int, entryID int) error {
	anilistErr := m.Platform.DeleteEntry(ctx, mediaID, entryID)
	if anilistErr != nil {
		m.enqueue(TargetAnilist, OpDeleteEntry, DeleteEntryPayload{MediaID: mediaID, EntryID: entryID})
	}

	if m.simklEnabled() && !isMangaMedia(ctx) {
		if err := m.simklClient.RemoveEntry(ctx, mediaID); err != nil {
			m.enqueue(TargetSimkl, OpDeleteEntry, DeleteEntryPayload{MediaID: mediaID, EntryID: entryID})
		}
	}

	return anilistErr
}

type AddToCollectionPayload struct {
	MediaIDs []int `json:"mediaIds"`
}

func (m *MirroringPlatform) AddMediaToCollection(ctx context.Context, mIds []int) error {
	anilistErr := m.Platform.AddMediaToCollection(ctx, mIds)
	if anilistErr != nil {
		m.enqueue(TargetAnilist, OpAddToCollection, AddToCollectionPayload{MediaIDs: mIds})
	}

	// AddMediaToCollection has no manga equivalent in platform.Platform (there is no
	// "addMangaToCollection" - manga has no downloaded-file collection concept), but the
	// isMangaMedia guard is kept here too for defense in depth if that ever changes.
	if m.simklEnabled() && !isMangaMedia(ctx) {
		var simklFailed bool
		for _, id := range mIds {
			if err := m.simklClient.AddToList(ctx, id, "plantowatch"); err != nil {
				simklFailed = true
			}
		}
		if simklFailed {
			m.enqueue(TargetSimkl, OpAddToCollection, AddToCollectionPayload{MediaIDs: mIds})
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
		NextAttemptAt: time.Now().Add(initialRetryDelay),
	})
}
