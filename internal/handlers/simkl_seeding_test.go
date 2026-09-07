package handlers

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSimklSeeding_ConcurrentRunsBothAccountedFor guards against the race the counter (rather
// than a plain presence flag) was introduced to fix: two overlapping "Sync Now" runs for the same
// profile (two tabs, a retried request) must both keep isSimklSeeding true until BOTH finish - the
// first one to finish must not clear seeding out from under the second, still-running one.
func TestSimklSeeding_ConcurrentRunsBothAccountedFor(t *testing.T) {
	h := &Handler{}
	const profileID = "_default"

	assert.False(t, h.isSimklSeeding(profileID), "must not report seeding before any run starts")

	h.simklSeedingStart(profileID)
	h.simklSeedingStart(profileID) // a second, overlapping run for the same profile
	assert.True(t, h.isSimklSeeding(profileID))

	h.simklSeedingFinish(profileID) // the first run finishes
	assert.True(t, h.isSimklSeeding(profileID), "must still report seeding while the second run is in flight")

	h.simklSeedingFinish(profileID) // the second run finishes
	assert.False(t, h.isSimklSeeding(profileID), "must report done once every in-flight run has finished")
}

// TestSimklSeeding_IsolatedPerProfile guards against one profile's seed state leaking into
// another's - HandleGetSimklSyncStatus must never report profile B as seeding just because
// profile A's sync is in flight on a shared multi-user instance.
func TestSimklSeeding_IsolatedPerProfile(t *testing.T) {
	h := &Handler{}

	h.simklSeedingStart("profile-a")
	assert.True(t, h.isSimklSeeding("profile-a"))
	assert.False(t, h.isSimklSeeding("profile-b"))

	h.simklSeedingFinish("profile-a")
	assert.False(t, h.isSimklSeeding("profile-a"))
}

// TestSimklSeeding_ConcurrentStartFinish is a race-detector exercise (run with -race) for the
// underlying sync.Map + atomic.Int32 pair under real concurrent access, not just the sequential
// interleaving above.
func TestSimklSeeding_ConcurrentStartFinish(t *testing.T) {
	h := &Handler{}
	const profileID = "_default"

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.simklSeedingStart(profileID)
			h.isSimklSeeding(profileID)
			h.simklSeedingFinish(profileID)
		}()
	}
	wg.Wait()

	assert.False(t, h.isSimklSeeding(profileID), "counter must settle back to zero once every goroutine's start/finish pair has run")
}
