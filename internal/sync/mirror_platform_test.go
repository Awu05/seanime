package sync

import (
	"context"
	"errors"
	"seanime/internal/api/anilist"
	"seanime/internal/api/simkl"
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
	updateProgressErr    error
	updateProgressCalls  int
	updateEntryCalls     int
	updateEntryErr       error
	updateRepeatCalls    int
	updateRepeatErr      error
	deleteEntryCalls     int
	deleteEntryErr       error
	addToCollectionCalls int
	addToCollectionErr   error
}

func (f *fakePlatform) UpdateEntryProgress(ctx context.Context, mediaID int, progress int, totalEpisodes *int) error {
	f.updateProgressCalls++
	return f.updateProgressErr
}

func (f *fakePlatform) UpdateEntry(ctx context.Context, mediaID int, status *anilist.MediaListStatus, scoreRaw *int, progress *int, startedAt *anilist.FuzzyDateInput, completedAt *anilist.FuzzyDateInput) error {
	f.updateEntryCalls++
	return f.updateEntryErr
}

func (f *fakePlatform) UpdateEntryRepeat(ctx context.Context, mediaID int, repeat int) error {
	f.updateRepeatCalls++
	return f.updateRepeatErr
}

func (f *fakePlatform) DeleteEntry(ctx context.Context, mediaID int, entryID int) error {
	f.deleteEntryCalls++
	return f.deleteEntryErr
}

func (f *fakePlatform) AddMediaToCollection(ctx context.Context, mIds []int) error {
	f.addToCollectionCalls++
	return f.addToCollectionErr
}

type fakeSimklClient struct {
	markProgressErr   error
	markProgressCalls int
	lastAnilistID     int
	lastEpisode       int
	addToListCalls    int
	addToListErr      error
	lastStatus        string
	setRatingCalls    int
	setRatingErr      error
	lastRating        int
	removeRatingCalls int
	removeRatingErr   error
	removeEntryCalls  int
	removeEntryErr    error
}

func (f *fakeSimklClient) AddToList(ctx context.Context, anilistID int, status string) error {
	f.addToListCalls++
	f.lastStatus = status
	return f.addToListErr
}
func (f *fakeSimklClient) MarkProgress(ctx context.Context, anilistID int, episode int) error {
	f.markProgressCalls++
	f.lastAnilistID = anilistID
	f.lastEpisode = episode
	return f.markProgressErr
}
func (f *fakeSimklClient) RemoveEntry(ctx context.Context, anilistID int) error {
	f.removeEntryCalls++
	return f.removeEntryErr
}
func (f *fakeSimklClient) SetRating(ctx context.Context, anilistID int, rating int) error {
	f.setRatingCalls++
	f.lastRating = rating
	return f.setRatingErr
}
func (f *fakeSimklClient) RemoveRating(ctx context.Context, anilistID int) error {
	f.removeRatingCalls++
	return f.removeRatingErr
}
func (f *fakeSimklClient) TestConnection(ctx context.Context) error { return nil }
func (f *fakeSimklClient) GetAllItems(ctx context.Context) ([]simkl.AllItemsEntry, error) {
	return nil, nil
}

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

func TestMirroringPlatform_UpdateEntryProgress_ZeroProgress_SkipsSimkl(t *testing.T) {
	// MarkProgress(ctx, id, 0) builds an empty episode list, producing a request body
	// byte-identical to RemoveEntry's - it would DELETE the SIMKL backup entry. A progress
	// reset must be a no-op on the backup, so the mirror call is skipped entirely.
	inner := &fakePlatform{}
	simklClient := &fakeSimklClient{}
	queue := &fakeQueue{}
	mp := NewMirroringPlatform(inner, simklClient, queue, "_default", func() bool { return true })

	err := mp.UpdateEntryProgress(context.Background(), 101922, 0, nil)
	require.NoError(t, err)

	assert.Equal(t, 1, inner.updateProgressCalls, "the AniList reset must still go through")
	assert.Zero(t, simklClient.markProgressCalls, "progress 0 must never reach SIMKL")
	assert.Zero(t, simklClient.removeEntryCalls, "and must not be turned into a deletion either")
	assert.Empty(t, queue.enqueued)
}

func TestMirroringPlatform_RawPlatform_Unwraps(t *testing.T) {
	// The retry worker delivers AniList rows through RawPlatform: replaying through the
	// wrapper would re-enter interception and enqueue a duplicate row per failed attempt.
	inner := &fakePlatform{}
	mp := NewMirroringPlatform(inner, &fakeSimklClient{}, &fakeQueue{}, "_default", func() bool { return true })

	assert.Same(t, inner, RawPlatform(mp), "RawPlatform must return the wrapped-in platform")
	assert.Same(t, inner, RawPlatform(inner), "an unwrapped platform must pass through unchanged")
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

func TestMirroringPlatform_UpdateEntry_MirrorsStatusAndScore(t *testing.T) {
	inner := &fakePlatform{}
	simklClient := &fakeSimklClient{}
	queue := &fakeQueue{}
	mp := NewMirroringPlatform(inner, simklClient, queue, "_default", func() bool { return true })

	status := anilist.MediaListStatusCompleted
	score := 85
	err := mp.UpdateEntry(context.Background(), 101922, &status, &score, nil, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, 1, inner.updateEntryCalls)
	assert.Equal(t, 1, simklClient.addToListCalls)
	assert.Equal(t, "completed", simklClient.lastStatus)
	assert.Equal(t, 1, simklClient.setRatingCalls)
	assert.Equal(t, 9, simklClient.lastRating) // round(85/10)
	assert.Zero(t, simklClient.removeRatingCalls)
}

func TestMirroringPlatform_UpdateEntry_ZeroScoreRemovesRating(t *testing.T) {
	inner := &fakePlatform{}
	simklClient := &fakeSimklClient{}
	queue := &fakeQueue{}
	mp := NewMirroringPlatform(inner, simklClient, queue, "_default", func() bool { return true })

	status := anilist.MediaListStatusCurrent
	score := 0
	err := mp.UpdateEntry(context.Background(), 101922, &status, &score, nil, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, 1, simklClient.removeRatingCalls)
	assert.Zero(t, simklClient.setRatingCalls)
}

func TestMirroringPlatform_UpdateEntryRepeat_AnilistFails_Queued(t *testing.T) {
	wantErr := errors.New("anilist unreachable")
	inner := &fakePlatform{updateRepeatErr: wantErr}
	queue := &fakeQueue{}
	mp := NewMirroringPlatform(inner, &fakeSimklClient{}, queue, "_default", func() bool { return false })

	err := mp.UpdateEntryRepeat(context.Background(), 101922, 2)
	require.ErrorIs(t, err, wantErr)
	require.Len(t, queue.enqueued, 1)
	assert.Equal(t, "update_repeat", queue.enqueued[0].Operation)
}

func TestMirroringPlatform_DeleteEntry_MirrorsRemoval(t *testing.T) {
	inner := &fakePlatform{}
	simklClient := &fakeSimklClient{}
	queue := &fakeQueue{}
	mp := NewMirroringPlatform(inner, simklClient, queue, "_default", func() bool { return true })

	err := mp.DeleteEntry(context.Background(), 101922, 555)
	require.NoError(t, err)
	assert.Equal(t, 1, inner.deleteEntryCalls)
	assert.Equal(t, 1, simklClient.removeEntryCalls)
	assert.Empty(t, queue.enqueued)
}

func TestMirroringPlatform_AddMediaToCollection_AnilistFails_Queued(t *testing.T) {
	wantErr := errors.New("anilist unreachable")
	inner := &fakePlatform{addToCollectionErr: wantErr}
	queue := &fakeQueue{}
	mp := NewMirroringPlatform(inner, &fakeSimklClient{}, queue, "_default", func() bool { return false })

	err := mp.AddMediaToCollection(context.Background(), []int{101922, 21})
	require.ErrorIs(t, err, wantErr)
	require.Len(t, queue.enqueued, 1)
	assert.Equal(t, "add_to_collection", queue.enqueued[0].Operation)
}

// Manga list mutations must never reach SIMKL: SIMKL only tracks anime, and
// platform.Platform's mutating methods carry no media-type argument to tell the two apart -
// see WithMangaMedia.

func TestMirroringPlatform_UpdateEntry_MangaMedia_SkipsSimkl(t *testing.T) {
	inner := &fakePlatform{}
	simklClient := &fakeSimklClient{}
	queue := &fakeQueue{}
	mp := NewMirroringPlatform(inner, simklClient, queue, "_default", func() bool { return true })

	status := anilist.MediaListStatusCompleted
	score := 85
	ctx := WithMangaMedia(context.Background())
	err := mp.UpdateEntry(ctx, 101922, &status, &score, nil, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, 1, inner.updateEntryCalls, "the real AniList call must still go through")
	assert.Zero(t, simklClient.addToListCalls, "manga must never be mirrored to SIMKL's anime endpoints")
	assert.Zero(t, simklClient.setRatingCalls)
	assert.Empty(t, queue.enqueued)
}

func TestMirroringPlatform_UpdateEntryProgress_MangaMedia_SkipsSimkl(t *testing.T) {
	inner := &fakePlatform{}
	simklClient := &fakeSimklClient{}
	queue := &fakeQueue{}
	mp := NewMirroringPlatform(inner, simklClient, queue, "_default", func() bool { return true })

	ctx := WithMangaMedia(context.Background())
	err := mp.UpdateEntryProgress(ctx, 101922, 5, nil)
	require.NoError(t, err)

	assert.Equal(t, 1, inner.updateProgressCalls)
	assert.Zero(t, simklClient.markProgressCalls, "manga reading progress must never be mirrored to SIMKL")
}

func TestMirroringPlatform_DeleteEntry_MangaMedia_SkipsSimkl(t *testing.T) {
	inner := &fakePlatform{}
	simklClient := &fakeSimklClient{}
	queue := &fakeQueue{}
	mp := NewMirroringPlatform(inner, simklClient, queue, "_default", func() bool { return true })

	ctx := WithMangaMedia(context.Background())
	err := mp.DeleteEntry(ctx, 101922, 555)
	require.NoError(t, err)

	assert.Equal(t, 1, inner.deleteEntryCalls)
	assert.Zero(t, simklClient.removeEntryCalls, "deleting a manga entry must not delete a SIMKL anime entry")
}
