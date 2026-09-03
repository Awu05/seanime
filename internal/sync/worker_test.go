package sync

import (
	"context"
	"seanime/internal/api/simkl"
	"seanime/internal/database/models"
	"seanime/internal/platforms/platform"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

	assert.Equal(t, 1, simklClient.markProgressCalls)
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
	simklClient := &fakeSimklClient{markProgressErr: assertAnError}

	w := NewWorker(store, func(profileID string) (simkl.Client, bool) { return simklClient, true }, func(profileID string) platform.Platform { return &fakePlatform{} }, func() bool { return true })
	w.FlushOnce(context.Background())

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

	assert.Equal(t, 1, clientA.markProgressCalls, "profileA's row must be delivered through profileA's client")
	assert.Equal(t, 1, clientB.markProgressCalls, "profileB's row must be delivered through profileB's client")
}
