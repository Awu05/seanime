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
	rows, err := w.store.GetDuePendingSyncs(target, pendingSyncBatchSize)
	if err != nil {
		return
	}
	for _, row := range rows {
		var deliverErr error
		switch target {
		case TargetSimkl:
			deliverErr = w.deliverToSimkl(ctx, row)
		case TargetAnilist:
			deliverErr = w.deliverToAnilist(ctx, row)
		}

		if deliverErr != nil {
			_ = w.store.IncrementPendingSyncAttempt(row.ID, time.Now().Add(backoffFor(row.Attempts)), deliverErr.Error())
			continue
		}
		_ = w.store.DeletePendingSync(row.ID)
	}
}

func (w *Worker) deliverToSimkl(ctx context.Context, row *models.PendingSync) error {
	client, ok := w.simklClientFor(row.ProfileID)
	if !ok {
		return errors.New("simkl: not connected for profile " + row.ProfileID)
	}
	switch row.Operation {
	case OpUpdateProgress:
		var p UpdateProgressPayload
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return err
		}
		return client.MarkProgress(ctx, p.MediaID, p.Progress)
	case OpDeleteEntry:
		var p DeleteEntryPayload
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return err
		}
		return client.RemoveEntry(ctx, p.MediaID)
	case OpUpdateEntry:
		var p UpdateEntryPayload
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return err
		}
		if p.Status != nil {
			if err := client.AddToList(ctx, p.MediaID, MapAnilistStatusToSimkl(*p.Status)); err != nil {
				return err
			}
		}
		if p.ScoreRaw != nil {
			rating, shouldRemove := MapAnilistScoreToSimklRating(*p.ScoreRaw)
			if shouldRemove {
				return client.RemoveRating(ctx, p.MediaID)
			}
			return client.SetRating(ctx, p.MediaID, rating)
		}
		return nil
	case OpAddToCollection:
		var p AddToCollectionPayload
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return err
		}
		for _, id := range p.MediaIDs {
			if err := client.AddToList(ctx, id, "plantowatch"); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("simkl: unknown pending-sync operation %q", row.Operation)
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
