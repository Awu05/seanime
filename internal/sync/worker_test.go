package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"seanime/internal/api/anilist"
	"seanime/internal/api/simkl"
	"seanime/internal/database/models"
	"seanime/internal/platforms/platform"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	rows           map[string][]*models.PendingSync
	incrementCalls []uint
	deletedIDs     []uint
	nextID         uint
}

func newFakeStore() *fakeStore {
	return &fakeStore{rows: map[string][]*models.PendingSync{}, nextID: 1}
}

func (s *fakeStore) addRow(target string, deliverErr bool) *models.PendingSync {
	row := &models.PendingSync{ProfileID: "_default", Target: target, Operation: "update_progress", Payload: []byte(`{"mediaId":1,"progress":1}`)}
	row.ID = s.nextID
	s.nextID++
	s.rows[target] = append(s.rows[target], row)
	return row
}

func (s *fakeStore) GetDuePendingSyncs(target string, limit int) ([]*models.PendingSync, error) {
	return s.rows[target], nil
}

func (s *fakeStore) IncrementPendingSyncAttempt(id uint, nextAttemptAt time.Time, lastErr string) error {
	s.incrementCalls = append(s.incrementCalls, id)
	return nil
}

func (s *fakeStore) DeletePendingSync(id uint) error {
	s.deletedIDs = append(s.deletedIDs, id)
	return nil
}

func TestWorker_FlushesSimklRowsRegardlessOfAnilistHealth(t *testing.T) {
	store := newFakeStore()
	row := store.addRow("simkl", false)
	simklClient := &fakeSimklClient{}
	inner := &fakePlatform{}

	w := NewWorker(store, func(profileID string) (simkl.Client, bool) { return simklClient, true }, func(profileID string) platform.Platform { return inner }, func() bool { return false })
	w.FlushOnce(context.Background())

	assert.Len(t, simklClient.markProgressBatchCalls, 1, "must deliver via the batch endpoint even for a single row")
	assert.Contains(t, store.deletedIDs, row.ID)
}

func TestWorker_SkipsAnilistRowsWhenUnhealthy(t *testing.T) {
	store := newFakeStore()
	store.addRow("anilist", false)
	inner := &fakePlatform{}

	w := NewWorker(store, func(profileID string) (simkl.Client, bool) { return &fakeSimklClient{}, true }, func(profileID string) platform.Platform { return inner }, func() bool { return false })
	w.FlushOnce(context.Background())

	assert.Zero(t, inner.updateProgressCalls, "must not retry AniList while circuit breaker reports unhealthy")
	assert.Empty(t, store.deletedIDs)
	assert.Empty(t, store.incrementCalls)
}

func TestWorker_RetriesAnilistRowsWhenHealthy(t *testing.T) {
	store := newFakeStore()
	row := store.addRow("anilist", false)
	inner := &fakePlatform{}

	w := NewWorker(store, func(profileID string) (simkl.Client, bool) { return &fakeSimklClient{}, true }, func(profileID string) platform.Platform { return inner }, func() bool { return true })
	w.FlushOnce(context.Background())

	assert.Equal(t, 1, inner.updateProgressCalls)
	assert.Contains(t, store.deletedIDs, row.ID)
}

func TestWorker_FailedDeliveryIncrementsAttemptInsteadOfDeleting(t *testing.T) {
	store := newFakeStore()
	row := store.addRow("simkl", true)
	simklClient := &fakeSimklClient{markProgressBatchErr: assertAnError}

	w := NewWorker(store, func(profileID string) (simkl.Client, bool) { return simklClient, true }, func(profileID string) platform.Platform { return &fakePlatform{} }, func() bool { return true })
	w.FlushOnce(context.Background())

	assert.Contains(t, store.incrementCalls, row.ID)
	assert.NotContains(t, store.deletedIDs, row.ID)
}

func addProgressRow(store *fakeStore, id uint, mediaID int) *models.PendingSync {
	row := &models.PendingSync{ProfileID: "_default", Target: "simkl", Operation: OpUpdateProgress,
		Payload: []byte(fmt.Sprintf(`{"mediaId":%d,"progress":1}`, mediaID))}
	row.ID = id
	store.rows["simkl"] = append(store.rows["simkl"], row)
	return row
}

func TestWorker_BatchesMultipleRowsIntoOneCall(t *testing.T) {
	store := newFakeStore()
	addProgressRow(store, 1, 101)
	addProgressRow(store, 2, 102)
	addProgressRow(store, 3, 103)
	simklClient := &fakeSimklClient{}

	w := NewWorker(store, func(profileID string) (simkl.Client, bool) { return simklClient, true }, func(profileID string) platform.Platform { return &fakePlatform{} }, func() bool { return true })
	w.FlushOnce(context.Background())

	require.Len(t, simklClient.markProgressBatchCalls, 1, "3 rows for the same action must be delivered in a single batched call, not 3 separate ones")
	assert.Len(t, simklClient.markProgressBatchCalls[0], 3)
	assert.Len(t, store.deletedIDs, 3)
}

func TestWorker_ChunksBatchesAtMaxBatchSize(t *testing.T) {
	store := newFakeStore()
	n := simkl.MaxBatchSize + 5
	for i := 1; i <= n; i++ {
		addProgressRow(store, uint(i), i)
	}
	simklClient := &fakeSimklClient{}

	w := NewWorker(store, func(profileID string) (simkl.Client, bool) { return simklClient, true }, func(profileID string) platform.Platform { return &fakePlatform{} }, func() bool { return true })
	w.FlushOnce(context.Background())

	require.Len(t, simklClient.markProgressBatchCalls, 2, "must split into two calls once the batch exceeds simkl.MaxBatchSize")
	assert.Len(t, simklClient.markProgressBatchCalls[0], simkl.MaxBatchSize)
	assert.Len(t, simklClient.markProgressBatchCalls[1], 5)
	assert.Len(t, store.deletedIDs, n)
}

func TestWorker_BatchFailureIncrementsEveryRowInThatBatch(t *testing.T) {
	store := newFakeStore()
	addProgressRow(store, 1, 101)
	addProgressRow(store, 2, 102)
	addProgressRow(store, 3, 103)
	simklClient := &fakeSimklClient{markProgressBatchErr: assertAnError}

	w := NewWorker(store, func(profileID string) (simkl.Client, bool) { return simklClient, true }, func(profileID string) platform.Platform { return &fakePlatform{} }, func() bool { return true })
	w.FlushOnce(context.Background())

	assert.ElementsMatch(t, []uint{1, 2, 3}, store.incrementCalls, "a failed batch call must retry every row that contributed to it")
	assert.Empty(t, store.deletedIDs)
}

func TestWorker_UpdateEntryRow_DeletedOnlyWhenBothActionsSucceed(t *testing.T) {
	store := newFakeStore()
	status := anilist.MediaListStatusCompleted
	score := 85
	payload, err := json.Marshal(UpdateEntryPayload{MediaID: 101922, Status: &status, ScoreRaw: &score})
	require.NoError(t, err)
	row := &models.PendingSync{ProfileID: "_default", Target: "simkl", Operation: OpUpdateEntry, Payload: payload}
	row.ID = 1
	store.rows["simkl"] = []*models.PendingSync{row}

	// SetRatingBatch fails while AddToListBatch succeeds - the row carries both a status change
	// and a rating change, so it must still be retried since not every action it needed succeeded
	// (retrying is harmless: AddToList is idempotent, so redoing the already-applied status
	// change costs nothing).
	simklClient := &fakeSimklClient{setRatingBatchErr: assertAnError}
	w := NewWorker(store, func(profileID string) (simkl.Client, bool) { return simklClient, true }, func(profileID string) platform.Platform { return &fakePlatform{} }, func() bool { return true })
	w.FlushOnce(context.Background())

	assert.Len(t, simklClient.addToListBatchCalls, 1, "the status action must still be attempted independently of the rating action")
	assert.Contains(t, store.incrementCalls, row.ID)
	assert.NotContains(t, store.deletedIDs, row.ID)
}

var assertAnError = assertError("simkl still down")

type assertError string

func (e assertError) Error() string { return string(e) }

func TestWorker_RoutesDeliveryPerProfile(t *testing.T) {
	store := newFakeStore()
	rowA := store.addRow("simkl", false)
	rowA.ProfileID = "profileA"
	rowB := &models.PendingSync{ProfileID: "profileB", Target: "simkl", Operation: "update_progress", Payload: []byte(`{"mediaId":2,"progress":2}`)}
	rowB.ID = 99
	store.rows["simkl"] = append(store.rows["simkl"], rowB)

	clientA := &fakeSimklClient{}
	clientB := &fakeSimklClient{}

	w := NewWorker(store,
		func(profileID string) (simkl.Client, bool) {
			switch profileID {
			case "profileA":
				return clientA, true
			case "profileB":
				return clientB, true
			default:
				return nil, false
			}
		},
		func(profileID string) platform.Platform { return &fakePlatform{} },
		func() bool { return true },
	)
	w.FlushOnce(context.Background())

	assert.Len(t, clientA.markProgressBatchCalls, 1, "profileA's row must be delivered through profileA's client")
	assert.Len(t, clientB.markProgressBatchCalls, 1, "profileB's row must be delivered through profileB's client")
}
