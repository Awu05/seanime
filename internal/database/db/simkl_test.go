package db

import (
	"seanime/internal/database/models"
	"seanime/internal/util"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSimklAccountRoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	database, err := NewDatabase(tempDir, "simkl_account_test", util.NewLogger())
	require.NoError(t, err)
	defer func() {
		sqlDB, _ := database.gormdb.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	}()

	_, err = database.GetSimklAccount("_default")
	require.Error(t, err, "expected an error when no account is connected yet")

	saved, err := database.UpsertSimklAccount(&models.SimklAccount{
		ProfileID:   "_default",
		Username:    "testuser",
		AccessToken: "token-123",
	})
	require.NoError(t, err)
	assert.Equal(t, "testuser", saved.Username)

	got, err := database.GetSimklAccount("_default")
	require.NoError(t, err)
	assert.Equal(t, "token-123", got.AccessToken)

	// Upsert again with the same profile should update, not duplicate.
	_, err = database.UpsertSimklAccount(&models.SimklAccount{
		ProfileID:   "_default",
		Username:    "testuser",
		AccessToken: "token-456",
	})
	require.NoError(t, err)
	got, err = database.GetSimklAccount("_default")
	require.NoError(t, err)
	assert.Equal(t, "token-456", got.AccessToken)

	require.NoError(t, database.DeleteSimklAccount("_default"))
	_, err = database.GetSimklAccount("_default")
	require.Error(t, err)
}

func TestSimklSettingsRoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	database, err := NewDatabase(tempDir, "simkl_settings_test", util.NewLogger())
	require.NoError(t, err)
	defer func() {
		sqlDB, _ := database.gormdb.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	}()

	defaults, err := database.GetSimklSettings("_default")
	require.NoError(t, err)
	assert.False(t, defaults.Enabled, "should default to disabled when no row exists yet")

	saved, err := database.UpsertSimklSettings(&models.SimklSettings{ProfileID: "_default", Enabled: true, ClientId: "abc123"})
	require.NoError(t, err)
	assert.True(t, saved.Enabled)
	assert.Equal(t, "abc123", saved.ClientId)

	got, err := database.GetSimklSettings("_default")
	require.NoError(t, err)
	assert.True(t, got.Enabled)
	assert.Equal(t, "abc123", got.ClientId)

	// A later upsert that only changes Enabled must not wipe out the previously saved ClientId -
	// UpsertSimklSettings always writes both columns from the struct it's given, so a caller that
	// fetches-then-modifies (as the handler does) is what actually protects against this; this
	// case asserts the DB layer preserves a value that IS included in a subsequent upsert.
	saved2, err := database.UpsertSimklSettings(&models.SimklSettings{ProfileID: "_default", Enabled: false, ClientId: "abc123"})
	require.NoError(t, err)
	assert.False(t, saved2.Enabled)
	assert.Equal(t, "abc123", saved2.ClientId)
}

func TestPendingSyncQueue(t *testing.T) {
	tempDir := t.TempDir()
	database, err := NewDatabase(tempDir, "pending_sync_test", util.NewLogger())
	require.NoError(t, err)
	defer func() {
		sqlDB, _ := database.gormdb.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	}()

	require.NoError(t, database.EnqueuePendingSync(&models.PendingSync{
		ProfileID:     "_default",
		Target:        "simkl",
		Operation:     "update_progress",
		Payload:       []byte(`{"mediaId":1}`),
		NextAttemptAt: time.Now().Add(-time.Minute), // already due
	}))
	require.NoError(t, database.EnqueuePendingSync(&models.PendingSync{
		ProfileID:     "_default",
		Target:        "anilist",
		Operation:     "update_progress",
		Payload:       []byte(`{"mediaId":2}`),
		NextAttemptAt: time.Now().Add(time.Hour), // not due yet
	}))

	due, err := database.GetDuePendingSyncs("simkl", 10)
	require.NoError(t, err)
	require.Len(t, due, 1)
	assert.Equal(t, "update_progress", due[0].Operation)

	notYetDue, err := database.GetDuePendingSyncs("anilist", 10)
	require.NoError(t, err)
	assert.Empty(t, notYetDue, "row scheduled an hour from now should not be due")

	require.NoError(t, database.IncrementPendingSyncAttempt(due[0].ID, time.Now().Add(time.Minute), "boom"))
	stillDue, err := database.GetDuePendingSyncs("simkl", 10)
	require.NoError(t, err)
	assert.Empty(t, stillDue, "row should not be due again until its new next_attempt_at")

	require.NoError(t, database.DeletePendingSync(due[0].ID))
}

func TestCountPendingSyncs(t *testing.T) {
	tempDir := t.TempDir()
	database, err := NewDatabase(tempDir, "count_pending_sync_test", util.NewLogger())
	require.NoError(t, err)
	defer func() {
		sqlDB, _ := database.gormdb.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	}()

	require.NoError(t, database.EnqueuePendingSync(&models.PendingSync{
		ProfileID: "_default", Target: "simkl", Operation: "update_progress",
		Payload: []byte(`{"mediaId":1}`), NextAttemptAt: time.Now(),
	}))
	require.NoError(t, database.EnqueuePendingSync(&models.PendingSync{
		ProfileID: "_default", Target: "simkl", Operation: "update_progress",
		Payload: []byte(`{"mediaId":2}`), NextAttemptAt: time.Now(),
	}))
	require.NoError(t, database.EnqueuePendingSync(&models.PendingSync{
		ProfileID: "_default", Target: "anilist", Operation: "update_progress",
		Payload: []byte(`{"mediaId":3}`), NextAttemptAt: time.Now(),
	}))
	require.NoError(t, database.EnqueuePendingSync(&models.PendingSync{
		ProfileID: "other-profile", Target: "simkl", Operation: "update_progress",
		Payload: []byte(`{"mediaId":4}`), NextAttemptAt: time.Now(),
	}))

	count, err := database.CountPendingSyncs("_default", "simkl")
	require.NoError(t, err)
	assert.EqualValues(t, 2, count, "must count only this profile's simkl-target rows")

	// A row that exhausted its retry budget is never deleted (see IncrementPendingSyncAttempt),
	// but it must not count here - otherwise a "syncing..." indicator built on this count would
	// get stuck forever the moment even one row permanently fails.
	rows, err := database.GetDuePendingSyncs("simkl", 10)
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	exhaustedID := rows[0].ID
	for i := 0; i < maxPendingSyncAttempts; i++ {
		require.NoError(t, database.IncrementPendingSyncAttempt(exhaustedID, time.Now(), "still failing"))
	}

	count, err = database.CountPendingSyncs("_default", "simkl")
	require.NoError(t, err)
	assert.EqualValues(t, 1, count, "an exhausted row must not be counted as still pending")
}
