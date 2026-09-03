package sync

import (
	"context"
	"errors"
	"seanime/internal/api/anilist"
	"seanime/internal/database/models"
	"seanime/internal/platforms/platform"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePlatform implements platform.Platform by embedding the interface (nil) so only the
// methods a test actually needs are overridden - matches the pattern already used for
// countingAnimeCollectionClient in internal/platforms/anilist_platform.
type fakePlatform struct {
	platform.Platform
	updateProgressErr   error
	updateProgressCalls int
}

func (f *fakePlatform) UpdateEntryProgress(ctx context.Context, mediaID int, progress int, totalEpisodes *int) error {
	f.updateProgressCalls++
	return f.updateProgressErr
}

type fakeSimklClient struct {
	markProgressErr   error
	markProgressCalls int
	lastAnilistID     int
	lastEpisode       int
}

func (f *fakeSimklClient) AddToList(ctx context.Context, anilistID int, status string) error {
	return nil
}
func (f *fakeSimklClient) MarkProgress(ctx context.Context, anilistID int, episode int) error {
	f.markProgressCalls++
	f.lastAnilistID = anilistID
	f.lastEpisode = episode
	return f.markProgressErr
}
func (f *fakeSimklClient) RemoveEntry(ctx context.Context, anilistID int) error           { return nil }
func (f *fakeSimklClient) SetRating(ctx context.Context, anilistID int, rating int) error { return nil }
func (f *fakeSimklClient) RemoveRating(ctx context.Context, anilistID int) error          { return nil }
func (f *fakeSimklClient) TestConnection(ctx context.Context) error                       { return nil }

type fakeQueue struct {
	enqueued []*models.PendingSync
}

func (f *fakeQueue) EnqueuePendingSync(item *models.PendingSync) error {
	f.enqueued = append(f.enqueued, item)
	return nil
}

func TestMirroringPlatform_UpdateEntryProgress_AllSucceed(t *testing.T) {
	inner := &fakePlatform{}
	simklClient := &fakeSimklClient{}
	queue := &fakeQueue{}
	mp := NewMirroringPlatform(inner, simklClient, queue, "_default", func() bool { return true })

	err := mp.UpdateEntryProgress(context.Background(), 101922, 5, nil)
	require.NoError(t, err)

	assert.Equal(t, 1, inner.updateProgressCalls)
	assert.Equal(t, 1, simklClient.markProgressCalls)
	assert.Equal(t, 101922, simklClient.lastAnilistID)
	assert.Equal(t, 5, simklClient.lastEpisode)
	assert.Empty(t, queue.enqueued, "nothing should be queued when both calls succeed")
}

func TestMirroringPlatform_UpdateEntryProgress_AnilistFails_StillMirrorsAndQueues(t *testing.T) {
	wantErr := errors.New("anilist unreachable")
	inner := &fakePlatform{updateProgressErr: wantErr}
	simklClient := &fakeSimklClient{}
	queue := &fakeQueue{}
	mp := NewMirroringPlatform(inner, simklClient, queue, "_default", func() bool { return true })

	err := mp.UpdateEntryProgress(context.Background(), 101922, 5, nil)
	require.ErrorIs(t, err, wantErr, "the original AniList error must still reach the caller")

	assert.Equal(t, 1, simklClient.markProgressCalls, "SIMKL mirror must still fire even though AniList failed")
	require.Len(t, queue.enqueued, 1)
	assert.Equal(t, "anilist", queue.enqueued[0].Target)
	assert.Equal(t, "update_progress", queue.enqueued[0].Operation)
}

func TestMirroringPlatform_UpdateEntryProgress_SimklFails_QueuedNotReturnedAsError(t *testing.T) {
	inner := &fakePlatform{}
	simklClient := &fakeSimklClient{markProgressErr: errors.New("simkl down")}
	queue := &fakeQueue{}
	mp := NewMirroringPlatform(inner, simklClient, queue, "_default", func() bool { return true })

	err := mp.UpdateEntryProgress(context.Background(), 101922, 5, nil)
	require.NoError(t, err, "a SIMKL failure must never surface to the caller")

	require.Len(t, queue.enqueued, 1)
	assert.Equal(t, "simkl", queue.enqueued[0].Target)
}

func TestMirroringPlatform_UpdateEntryProgress_SimklDisabled_NeverCalled(t *testing.T) {
	inner := &fakePlatform{}
	simklClient := &fakeSimklClient{}
	queue := &fakeQueue{}
	mp := NewMirroringPlatform(inner, simklClient, queue, "_default", func() bool { return false })

	err := mp.UpdateEntryProgress(context.Background(), 101922, 5, nil)
	require.NoError(t, err)
	assert.Zero(t, simklClient.markProgressCalls)
	assert.Empty(t, queue.enqueued)
}

func TestMirroringPlatform_PassthroughReadMethod(t *testing.T) {
	// GetAnime is not overridden by MirroringPlatform - embedding must delegate to inner.
	inner := &fakePlatformWithGetAnime{}
	mp := NewMirroringPlatform(inner, &fakeSimklClient{}, &fakeQueue{}, "_default", func() bool { return false })

	got, err := mp.GetAnime(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, 1, got.ID)
}

type fakePlatformWithGetAnime struct {
	fakePlatform
}

func (f *fakePlatformWithGetAnime) GetAnime(ctx context.Context, mediaID int) (*anilist.BaseAnime, error) {
	return &anilist.BaseAnime{ID: mediaID}, nil
}
