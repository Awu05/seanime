package sync

import (
	"context"
	"encoding/json"
	"errors"
	"seanime/internal/api/anilist"
	"seanime/internal/api/simkl"
	"seanime/internal/database/models"
	"seanime/internal/platforms/platform"
	"time"
)

// errSimklMirrorFailed is a sentinel passed through mirrorToSimkl's doSimkl closures - the enqueue
// path only needs to know whether the mirror failed, never why, so no error detail is lost by
// collapsing multiple possible SIMKL call failures into one signal.
var errSimklMirrorFailed = errors.New("simkl: mirror failed")

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
	payload := UpdateProgressPayload{MediaID: mediaID, Progress: progress, TotalEpisodes: totalEpisodes}
	if anilistErr != nil {
		m.enqueue(TargetAnilist, OpUpdateProgress, payload)
	}

	// progress <= 0 is skipped on the SIMKL side: MarkProgress(0) builds an empty episode list,
	// which is byte-identical to RemoveEntry's request body and would DELETE the backup entry.
	// A progress reset must be a no-op on the backup, never a deletion.
	if progress > 0 {
		m.mirrorToSimkl(ctx, OpUpdateProgress, payload, func() error {
			return m.simklClient.MarkProgress(ctx, mediaID, progress)
		})
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

	m.mirrorToSimkl(ctx, OpUpdateEntry, payload, func() error {
		var failed bool
		if status != nil {
			if err := m.simklClient.AddToList(ctx, mediaID, MapAnilistStatusToSimkl(*status)); err != nil {
				failed = true
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
				failed = true
			}
		}
		if failed {
			return errSimklMirrorFailed
		}
		return nil
	})

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
	payload := DeleteEntryPayload{MediaID: mediaID, EntryID: entryID}
	if anilistErr != nil {
		m.enqueue(TargetAnilist, OpDeleteEntry, payload)
	}

	m.mirrorToSimkl(ctx, OpDeleteEntry, payload, func() error {
		return m.simklClient.RemoveEntry(ctx, mediaID)
	})

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
		// Delivered via AddToListBatch in chunks of MaxBatchSize rather than one AddToList call
		// per id: SIMKL paces POSTs to 1/1.1s process-wide (see postLimiter in api/simkl/client.go),
		// so a per-item loop would serialize an N-item add over N*1.1s instead of one request per
		// chunk. Reuses deliverBatched (worker.go) - the retry Worker's own batch-chunking helper -
		// rather than a second hand-rolled loop; mIds double as deliverBatched's correlation ids
		// (there's no PendingSync row to key on here, but the media id itself works just as well
		// to identify which chunk a given failure belongs to).
		items := make([]simkl.AddToListItem, len(mIds))
		correlationIDs := make([]uint, len(mIds))
		for i, id := range mIds {
			items[i] = simkl.AddToListItem{AnilistID: id, Status: "plantowatch"}
			correlationIDs[i] = uint(id)
		}
		failed := make(map[uint]bool, len(mIds))
		deliverBatched(ctx, items, correlationIDs, func(id uint, _ error) { failed[id] = true }, m.simklClient.AddToListBatch)
		// Only the ids in a chunk that actually failed are re-enqueued - AddToList is idempotent
		// so re-sending a succeeded id would be harmless, but there's no reason to widen the retry
		// beyond the chunk that's known to have failed.
		if len(failed) > 0 {
			failedIDs := make([]int, 0, len(failed))
			for _, id := range mIds {
				if failed[uint(id)] {
					failedIDs = append(failedIDs, id)
				}
			}
			m.enqueue(TargetSimkl, OpAddToCollection, AddToCollectionPayload{MediaIDs: failedIDs})
		}
	}

	return anilistErr
}

// mirrorToSimkl runs doSimkl if SIMKL mirroring applies to ctx (enabled, not a manga mutation -
// see WithMangaMedia), enqueueing operation/payload for retry if it returns an error. Shared by
// every mutating method except AddMediaToCollection, which batches and needs to enqueue only the
// subset of ids that actually failed rather than the fixed payload passed in here.
func (m *MirroringPlatform) mirrorToSimkl(ctx context.Context, operation string, payload interface{}, doSimkl func() error) {
	if !m.simklEnabled() || isMangaMedia(ctx) {
		return
	}
	if err := doSimkl(); err != nil {
		m.enqueue(TargetSimkl, operation, payload)
	}
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
