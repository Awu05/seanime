package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"seanime/internal/api/simkl"
	"seanime/internal/database/models"
	"seanime/internal/platforms/platform"
	"time"
)

// PendingSyncStore is the subset of *db.Database this package depends on for the worker.
type PendingSyncStore interface {
	GetDuePendingSyncs(target string, limit int) ([]*models.PendingSync, error)
	IncrementPendingSyncAttempt(id uint, nextAttemptAt time.Time, lastErr string) error
	DeletePendingSync(id uint) error
}

const pendingSyncBatchSize = 20

// simklFlushRowLimit is how many due SIMKL rows one tick pulls from the store. Higher than
// pendingSyncBatchSize because batching (see flushSimklRows) turns this into at most
// simklFlushRowLimit/simkl.MaxBatchSize actual API calls per action type, not one call per row -
// a "sync now" backfill of thousands of entries would otherwise take hours to drain at one
// paced POST per row.
const simklFlushRowLimit = 250

// initialRetryDelay is how long a freshly-enqueued row waits before its first delivery
// attempt. Shared with MirroringPlatform.enqueue so a row's first wait and backoffFor(0)
// can't drift apart.
const initialRetryDelay = time.Minute

// backoffFor returns the delay before the next retry, capped at 30 minutes.
func backoffFor(attempts int) time.Duration {
	switch {
	case attempts <= 0:
		return initialRetryDelay
	case attempts == 1:
		return 5 * time.Minute
	case attempts == 2:
		return 15 * time.Minute
	default:
		return 30 * time.Minute
	}
}

// Worker flushes the PendingSync queue. simklClientFor and anilistPlatformFor resolve the
// correct per-profile client/platform for each row's ProfileID at delivery time — never a
// single fixed client/platform — so a multi-user deployment's retries are never delivered
// through the wrong profile's AniList/SIMKL account.
type Worker struct {
	store              PendingSyncStore
	simklClientFor     func(profileID string) (simkl.Client, bool)
	anilistPlatformFor func(profileID string) platform.Platform
	anilistHealthy     func() bool
}

func NewWorker(store PendingSyncStore, simklClientFor func(profileID string) (simkl.Client, bool), anilistPlatformFor func(profileID string) platform.Platform, anilistHealthy func() bool) *Worker {
	return &Worker{store: store, simklClientFor: simklClientFor, anilistPlatformFor: anilistPlatformFor, anilistHealthy: anilistHealthy}
}

// Run flushes due rows every interval until ctx is cancelled.
func (w *Worker) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.FlushOnce(ctx)
		}
	}
}

// FlushOnce attempts delivery of every currently-due row for both targets.
func (w *Worker) FlushOnce(ctx context.Context) {
	w.flushTarget(ctx, TargetSimkl)
	if w.anilistHealthy() {
		w.flushTarget(ctx, TargetAnilist)
	}
}

func (w *Worker) flushTarget(ctx context.Context, target string) {
	if target == TargetSimkl {
		rows, err := w.store.GetDuePendingSyncs(target, simklFlushRowLimit)
		if err != nil {
			return
		}
		w.flushSimklRows(ctx, rows)
		return
	}

	rows, err := w.store.GetDuePendingSyncs(target, pendingSyncBatchSize)
	if err != nil {
		return
	}
	for _, row := range rows {
		if deliverErr := w.deliverToAnilist(ctx, row); deliverErr != nil {
			_ = w.store.IncrementPendingSyncAttempt(row.ID, time.Now().Add(backoffFor(row.Attempts)), deliverErr.Error())
			continue
		}
		_ = w.store.DeletePendingSync(row.ID)
	}
}

// flushSimklRows delivers every due SIMKL row using as few API calls as possible - SIMKL's
// write endpoints accept up to simkl.MaxBatchSize items per call, and its docs explicitly
// recommend batching over pacing many single-item requests. Rows are grouped by profile first
// (each profile's rows must go out under that profile's own SIMKL client/token - never mixed
// into another profile's call), then by which SIMKL action they need.
//
// A row can require more than one action (OpUpdateEntry can carry both a status change and a
// rating change, delivered via two different endpoints) - rowErr/fail track failures per row ID
// across every action-batch a row contributed to, so the row is only considered delivered once
// ALL of its actions succeeded. AddToList/MarkProgress/SetRating/RemoveEntry/RemoveRating are
// all idempotent, so if one action for a row succeeds and another fails, retrying the whole row
// (redoing the already-succeeded action too) is harmless.
func (w *Worker) flushSimklRows(ctx context.Context, rows []*models.PendingSync) {
	byProfile := map[string][]*models.PendingSync{}
	for _, row := range rows {
		byProfile[row.ProfileID] = append(byProfile[row.ProfileID], row)
	}

	rowErr := map[uint]error{}
	fail := func(rowID uint, err error) {
		if _, already := rowErr[rowID]; !already {
			rowErr[rowID] = err
		}
	}

	for profileID, profileRows := range byProfile {
		client, ok := w.simklClientFor(profileID)
		if !ok {
			notConnected := errors.New("simkl: not connected for profile " + profileID)
			for _, row := range profileRows {
				fail(row.ID, notConnected)
			}
			continue
		}

		var addItems []simkl.AddToListItem
		var addRows []uint
		var progressItems []simkl.ProgressItem
		var progressRows []uint
		var removeIDs []int
		var removeRows []uint
		var ratingItems []simkl.RatingItem
		var ratingRows []uint
		var removeRatingIDs []int
		var removeRatingRows []uint

		for _, row := range profileRows {
			switch row.Operation {
			case OpUpdateProgress:
				var p UpdateProgressPayload
				if err := json.Unmarshal(row.Payload, &p); err != nil {
					fail(row.ID, err)
					continue
				}
				progressItems = append(progressItems, simkl.ProgressItem{AnilistID: p.MediaID, Episode: p.Progress})
				progressRows = append(progressRows, row.ID)
			case OpDeleteEntry:
				var p DeleteEntryPayload
				if err := json.Unmarshal(row.Payload, &p); err != nil {
					fail(row.ID, err)
					continue
				}
				removeIDs = append(removeIDs, p.MediaID)
				removeRows = append(removeRows, row.ID)
			case OpUpdateEntry:
				var p UpdateEntryPayload
				if err := json.Unmarshal(row.Payload, &p); err != nil {
					fail(row.ID, err)
					continue
				}
				if p.Status != nil {
					addItems = append(addItems, simkl.AddToListItem{AnilistID: p.MediaID, Status: MapAnilistStatusToSimkl(*p.Status)})
					addRows = append(addRows, row.ID)
				}
				if p.ScoreRaw != nil {
					rating, shouldRemove := MapAnilistScoreToSimklRating(*p.ScoreRaw)
					if shouldRemove {
						removeRatingIDs = append(removeRatingIDs, p.MediaID)
						removeRatingRows = append(removeRatingRows, row.ID)
					} else {
						ratingItems = append(ratingItems, simkl.RatingItem{AnilistID: p.MediaID, Rating: rating})
						ratingRows = append(ratingRows, row.ID)
					}
				}
			case OpAddToCollection:
				var p AddToCollectionPayload
				if err := json.Unmarshal(row.Payload, &p); err != nil {
					fail(row.ID, err)
					continue
				}
				for _, id := range p.MediaIDs {
					addItems = append(addItems, simkl.AddToListItem{AnilistID: id, Status: "plantowatch"})
					addRows = append(addRows, row.ID)
				}
			default:
				fail(row.ID, fmt.Errorf("simkl: unknown pending-sync operation %q", row.Operation))
			}
		}

		deliverBatched(ctx, addItems, addRows, fail, client.AddToListBatch)
		deliverBatched(ctx, progressItems, progressRows, fail, client.MarkProgressBatch)
		deliverBatched(ctx, removeIDs, removeRows, fail, client.RemoveEntryBatch)
		deliverBatched(ctx, ratingItems, ratingRows, fail, client.SetRatingBatch)
		deliverBatched(ctx, removeRatingIDs, removeRatingRows, fail, client.RemoveRatingBatch)
	}

	for _, row := range rows {
		if err, failed := rowErr[row.ID]; failed {
			_ = w.store.IncrementPendingSyncAttempt(row.ID, time.Now().Add(backoffFor(row.Attempts)), err.Error())
			continue
		}
		_ = w.store.DeletePendingSync(row.ID)
	}
}

// deliverBatched chunks items (and their correlated row IDs, same index) into groups of at most
// simkl.MaxBatchSize, calls send once per chunk, and on a chunk's failure records that error
// against every row that contributed an item to it.
func deliverBatched[T any](ctx context.Context, items []T, rowIDs []uint, fail func(uint, error), send func(context.Context, []T) error) {
	for start := 0; start < len(items); start += simkl.MaxBatchSize {
		end := start + simkl.MaxBatchSize
		if end > len(items) {
			end = len(items)
		}
		if err := send(ctx, items[start:end]); err != nil {
			for _, id := range rowIDs[start:end] {
				fail(id, err)
			}
		}
	}
}

func (w *Worker) deliverToAnilist(ctx context.Context, row *models.PendingSync) error {
	plat := w.anilistPlatformFor(row.ProfileID)
	if plat == nil {
		return errors.New("anilist: no platform available for profile " + row.ProfileID)
	}
	switch row.Operation {
	case OpUpdateProgress:
		var p UpdateProgressPayload
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return err
		}
		return plat.UpdateEntryProgress(ctx, p.MediaID, p.Progress, p.TotalEpisodes)
	case OpUpdateEntry:
		var p UpdateEntryPayload
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return err
		}
		return plat.UpdateEntry(ctx, p.MediaID, p.Status, p.ScoreRaw, p.Progress, p.StartedAt, p.CompletedAt)
	case OpUpdateRepeat:
		var p UpdateRepeatPayload
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return err
		}
		return plat.UpdateEntryRepeat(ctx, p.MediaID, p.Repeat)
	case OpDeleteEntry:
		var p DeleteEntryPayload
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return err
		}
		return plat.DeleteEntry(ctx, p.MediaID, p.EntryID)
	case OpAddToCollection:
		var p AddToCollectionPayload
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return err
		}
		return plat.AddMediaToCollection(ctx, p.MediaIDs)
	default:
		return fmt.Errorf("anilist: unknown pending-sync operation %q", row.Operation)
	}
}
