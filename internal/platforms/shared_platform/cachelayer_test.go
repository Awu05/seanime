package shared_platform

import (
	"context"
	"errors"
	"os"
	"seanime/internal/api/anilist"
	"seanime/internal/events"
	"seanime/internal/util"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gqlgo/gqlgenc/clientv2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

type cacheLayerTestClient struct {
	anilist.AnilistClient
	cacheDir            string
	animeCollection     *anilist.AnimeCollection
	mangaCollection     *anilist.MangaCollection
	updateEntryCalls    []cacheLayerUpdateEntryCall
	updateProgressCalls []cacheLayerUpdateProgressCall
	baseAnimeCalls      int32
	baseAnimeErr        error
	customQueryErr      error
}

type cacheLayerUpdateEntryCall struct {
	MediaID     *int
	Status      *anilist.MediaListStatus
	ScoreRaw    *int
	Progress    *int
	StartedAt   *anilist.FuzzyDateInput
	CompletedAt *anilist.FuzzyDateInput
}

type cacheLayerUpdateProgressCall struct {
	MediaID  *int
	Progress *int
	Status   *anilist.MediaListStatus
}

func (c *cacheLayerTestClient) IsAuthenticated() bool {
	return true
}

func (c *cacheLayerTestClient) GetCacheDir() string {
	return c.cacheDir
}

func (c *cacheLayerTestClient) BaseAnimeByID(_ context.Context, id *int, _ ...clientv2.RequestInterceptor) (*anilist.BaseAnimeByID, error) {
	atomic.AddInt32(&c.baseAnimeCalls, 1)
	if c.baseAnimeErr != nil {
		return nil, c.baseAnimeErr
	}
	mediaID := 0
	if id != nil {
		mediaID = *id
	}
	return &anilist.BaseAnimeByID{Media: &anilist.BaseAnime{ID: mediaID}}, nil
}

func (c *cacheLayerTestClient) CustomQuery(_ []byte, _ *zerolog.Logger, _ ...string) (interface{}, error) {
	if c.customQueryErr != nil {
		return nil, c.customQueryErr
	}
	return map[string]interface{}{"ok": true}, nil
}

func (c *cacheLayerTestClient) AnimeCollection(_ context.Context, _ *string, _ ...clientv2.RequestInterceptor) (*anilist.AnimeCollection, error) {
	return c.animeCollection, nil
}

func (c *cacheLayerTestClient) MangaCollection(_ context.Context, _ *string, _ ...clientv2.RequestInterceptor) (*anilist.MangaCollection, error) {
	return c.mangaCollection, nil
}

func (c *cacheLayerTestClient) UpdateMediaListEntry(_ context.Context, mediaID *int, status *anilist.MediaListStatus, scoreRaw *int, progress *int, startedAt *anilist.FuzzyDateInput, completedAt *anilist.FuzzyDateInput, _ ...clientv2.RequestInterceptor) (*anilist.UpdateMediaListEntry, error) {
	c.updateEntryCalls = append(c.updateEntryCalls, cacheLayerUpdateEntryCall{
		MediaID:     newCloned(mediaID),
		Status:      newCloned(status),
		ScoreRaw:    newCloned(scoreRaw),
		Progress:    newCloned(progress),
		StartedAt:   cloneFuzzyDateInput(startedAt),
		CompletedAt: cloneFuzzyDateInput(completedAt),
	})
	return &anilist.UpdateMediaListEntry{SaveMediaListEntry: &anilist.UpdateMediaListEntry_SaveMediaListEntry{ID: 999}}, nil
}

func (c *cacheLayerTestClient) UpdateMediaListEntryProgress(_ context.Context, mediaID *int, progress *int, status *anilist.MediaListStatus, _ ...clientv2.RequestInterceptor) (*anilist.UpdateMediaListEntryProgress, error) {
	c.updateProgressCalls = append(c.updateProgressCalls, cacheLayerUpdateProgressCall{
		MediaID:  newCloned(mediaID),
		Progress: newCloned(progress),
		Status:   newCloned(status),
	})
	return &anilist.UpdateMediaListEntryProgress{SaveMediaListEntry: &anilist.UpdateMediaListEntryProgress_SaveMediaListEntry{ID: 999}}, nil
}

func TestCacheLayerLogsOutOnInvalidToken(t *testing.T) {
	previousEventManager := events.GlobalWSEventManager
	events.GlobalWSEventManager = &events.GlobalWSEventManagerWrapper{}
	t.Cleanup(func() {
		events.GlobalWSEventManager = previousEventManager
		clearFailureTracking()
	})

	logoutCalled := make(chan struct{}, 1)
	cacheLayer := &CacheLayer{
		logoutFunc: func() {
			logoutCalled <- struct{}{}
		},
	}

	cacheLayer.checkAndUpdateWorkingState(errors.New("graphql: Invalid token"))

	select {
	case <-logoutCalled:
	case <-time.After(time.Second):
		t.Fatal("expected invalid token error to trigger logout")
	}
	require.Zero(t, getRecentFailureCount())
}

func TestCacheLayerQueuesProgressUpdateAndPatchesAnimeCache(t *testing.T) {
	client := &cacheLayerTestClient{
		cacheDir:        t.TempDir(),
		animeCollection: newTestAnimeCollection(101, 321, anilist.MediaListStatusCurrent, 2),
	}
	cacheLayer := newTestCacheLayer(t, client)

	// get the collection in cache
	_, err := cacheLayer.AnimeCollection(context.Background(), new("user"))
	require.NoError(t, err)

	IsWorking.Store(false)
	res, err := cacheLayer.UpdateMediaListEntryProgress(context.Background(), new(101), new(6), new(anilist.MediaListStatusCompleted))
	require.NoError(t, err)
	require.Equal(t, 321, res.GetSaveMediaListEntry().GetID())
	require.Empty(t, client.updateProgressCalls)

	// the cached entry should move lists immediately so refetches see the local change.
	cached := getCachedAnimeCollection(t, cacheLayer)
	entry, found := cached.GetListEntryFromAnimeId(101)
	require.True(t, found)
	require.Equal(t, 6, *entry.GetProgress())
	require.Equal(t, anilist.MediaListStatusCompleted, *entry.GetStatus())
	require.True(t, animeListContains(cached, anilist.MediaListStatusCompleted, 101))
	require.False(t, animeListContains(cached, anilist.MediaListStatusCurrent, 101))

	queued := getQueuedUpdate(t, cacheLayer, 101)
	require.Equal(t, 101, queued.MediaID)
	require.Equal(t, 6, *queued.Progress)
	require.Equal(t, anilist.MediaListStatusCompleted, *queued.Status)
	require.False(t, queued.FullUpdate)
}

func TestCacheLayerQueuesEntryUpdateAndSyncsWhenOnline(t *testing.T) {
	client := &cacheLayerTestClient{
		cacheDir:        t.TempDir(),
		mangaCollection: newTestMangaCollection(202, 654, anilist.MediaListStatusCurrent, 4),
	}
	cacheLayer := newTestCacheLayer(t, client)

	// seed manga cache, then queue an edit while the api is marked down
	_, err := cacheLayer.MangaCollection(context.Background(), new("user"))
	require.NoError(t, err)

	IsWorking.Store(false)
	startedAt := &anilist.FuzzyDateInput{Year: new(2025), Month: new(1), Day: new(2)}
	completedAt := &anilist.FuzzyDateInput{Year: new(2025), Month: new(2), Day: new(3)}
	res, err := cacheLayer.UpdateMediaListEntry(context.Background(), new(202), new(anilist.MediaListStatusCompleted), new(85), new(12), startedAt, completedAt)
	require.NoError(t, err)
	require.Equal(t, 654, res.GetSaveMediaListEntry().GetID())
	require.Empty(t, client.updateEntryCalls)

	cached := getCachedMangaCollection(t, cacheLayer)
	entry, found := cached.GetListEntryFromMangaId(202)
	require.True(t, found)
	require.Equal(t, 12, *entry.GetProgress())
	require.Equal(t, float64(85), *entry.GetScore())
	require.Equal(t, anilist.MediaListStatusCompleted, *entry.GetStatus())
	require.True(t, mangaListContains(cached, anilist.MediaListStatusCompleted, 202))
	require.False(t, mangaListContains(cached, anilist.MediaListStatusCurrent, 202))

	// when the api is healthy again, the queued full edit is flushed once and removed
	IsWorking.Store(true)
	cacheLayer.syncQueuedUpdates(context.Background())
	require.Len(t, client.updateEntryCalls, 1)
	require.Equal(t, 202, *client.updateEntryCalls[0].MediaID)
	require.Equal(t, anilist.MediaListStatusCompleted, *client.updateEntryCalls[0].Status)
	require.Equal(t, 85, *client.updateEntryCalls[0].ScoreRaw)
	require.Equal(t, 12, *client.updateEntryCalls[0].Progress)
	require.Equal(t, 2025, *client.updateEntryCalls[0].StartedAt.Year)
	require.Equal(t, 3, *client.updateEntryCalls[0].CompletedAt.Day)
	requireNoQueuedUpdate(t, cacheLayer, 202)
}

func TestCacheLayerLiveProgressUpdateClearsQueuedUpdate(t *testing.T) {
	client := &cacheLayerTestClient{
		cacheDir:        t.TempDir(),
		animeCollection: newTestAnimeCollection(101, 321, anilist.MediaListStatusCurrent, 2),
	}
	cacheLayer := newTestCacheLayer(t, client)

	_, err := cacheLayer.AnimeCollection(context.Background(), new("user"))
	require.NoError(t, err)

	// first update is queued while the api is marked down
	IsWorking.Store(false)
	_, err = cacheLayer.UpdateMediaListEntryProgress(context.Background(), new(101), new(6), new(anilist.MediaListStatusCompleted))
	require.NoError(t, err)
	queued := getQueuedUpdate(t, cacheLayer, 101)
	require.Equal(t, 6, *queued.Progress)

	// a later online update should win and remove the stale queued state
	IsWorking.Store(true)
	_, err = cacheLayer.UpdateMediaListEntryProgress(context.Background(), new(101), new(7), new(anilist.MediaListStatusCurrent))
	require.NoError(t, err)
	requireNoQueuedUpdate(t, cacheLayer, 101)

	cacheLayer.syncQueuedUpdates(context.Background())
	require.Len(t, client.updateProgressCalls, 1)
	require.Equal(t, 7, *client.updateProgressCalls[0].Progress)
}

func TestCacheLayerLiveEntryUpdateClearsQueuedUpdate(t *testing.T) {
	client := &cacheLayerTestClient{
		cacheDir:        t.TempDir(),
		mangaCollection: newTestMangaCollection(202, 654, anilist.MediaListStatusCurrent, 4),
	}
	cacheLayer := newTestCacheLayer(t, client)

	_, err := cacheLayer.MangaCollection(context.Background(), new("user"))
	require.NoError(t, err)

	// queue an older edit while AniList is unavailable
	IsWorking.Store(false)
	_, err = cacheLayer.UpdateMediaListEntry(context.Background(), new(202), new(anilist.MediaListStatusCompleted), new(80), new(12), nil, nil)
	require.NoError(t, err)
	queued := getQueuedUpdate(t, cacheLayer, 202)
	require.Equal(t, 80, *queued.ScoreRaw)

	// the successful online edit replaces it and should prevent stale replay
	IsWorking.Store(true)
	_, err = cacheLayer.UpdateMediaListEntry(context.Background(), new(202), new(anilist.MediaListStatusCurrent), new(90), new(13), nil, nil)
	require.NoError(t, err)
	requireNoQueuedUpdate(t, cacheLayer, 202)

	cacheLayer.syncQueuedUpdates(context.Background())
	require.Len(t, client.updateEntryCalls, 1)
	require.Equal(t, 90, *client.updateEntryCalls[0].ScoreRaw)
	require.Equal(t, 13, *client.updateEntryCalls[0].Progress)
}

func TestCacheLayerServesFreshDataWithoutHittingNetwork(t *testing.T) {
	// boundedCacheSet writes to the cache dir from a background goroutine after a successful
	// BaseAnimeByID call, so t.TempDir()'s auto-cleanup can race that write on Windows
	// ("directory is not empty"). Use a plain temp dir we don't remove instead.
	cacheDir, err := os.MkdirTemp("", "seanime-cachelayer-freshness-test-*")
	require.NoError(t, err)

	client := &cacheLayerTestClient{cacheDir: cacheDir}
	cacheLayer := newTestCacheLayer(t, client)
	cacheLayer.bucketTTLs[BaseAnimeBucket] = 50 * time.Millisecond

	id := 1

	_, err = cacheLayer.BaseAnimeByID(context.Background(), &id)
	require.NoError(t, err)
	require.EqualValues(t, 1, atomic.LoadInt32(&client.baseAnimeCalls), "expected 1 network call after the first fetch")

	// A repeated call within the TTL window should be served from cache, not the network.
	_, err = cacheLayer.BaseAnimeByID(context.Background(), &id)
	require.NoError(t, err)
	require.EqualValues(t, 1, atomic.LoadInt32(&client.baseAnimeCalls), "expected the cached call to skip the network")

	// Once the TTL expires, the next call should hit the network again.
	time.Sleep(80 * time.Millisecond)
	_, err = cacheLayer.BaseAnimeByID(context.Background(), &id)
	require.NoError(t, err)
	require.EqualValues(t, 2, atomic.LoadInt32(&client.baseAnimeCalls), "expected the stale entry to trigger a fresh network call")
}

// TestCacheLayerSurfacesNetworkErrorWhenNoCacheFallback guards a reported bug: when AniList
// requests fail and there's no cached fallback (e.g. an anime never fetched before, or a fresh
// cache), the real failure reason (e.g. "The AniList API has been temporarily disabled due to
// severe stability issues.") was being discarded and replaced with a generic "no cached data
// available" error, leaving the frontend with no way to show the user what actually went wrong.
func TestCacheLayerSurfacesNetworkErrorWhenNoCacheFallback(t *testing.T) {
	client := &cacheLayerTestClient{
		cacheDir:     t.TempDir(),
		baseAnimeErr: errors.New("The AniList API has been temporarily disabled due to severe stability issues."),
	}
	cacheLayer := newTestCacheLayer(t, client)

	id := 1
	_, err := cacheLayer.BaseAnimeByID(context.Background(), &id)
	require.Error(t, err)
	require.Contains(t, err.Error(), "The AniList API has been temporarily disabled due to severe stability issues.")
}

// TestCacheLayerCustomQuerySurfacesNetworkErrorWhenNoCacheFallback is the CustomQuery-specific
// counterpart to TestCacheLayerSurfacesNetworkErrorWhenNoCacheFallback - CustomQuery has its own
// hand-written cache-fallback logic rather than going through networkFirstGet.
func TestCacheLayerCustomQuerySurfacesNetworkErrorWhenNoCacheFallback(t *testing.T) {
	client := &cacheLayerTestClient{
		cacheDir:       t.TempDir(),
		customQueryErr: errors.New("The AniList API has been temporarily disabled due to severe stability issues."),
	}
	cacheLayer := newTestCacheLayer(t, client)

	_, err := cacheLayer.CustomQuery([]byte(`{"query":"{ Viewer { id } }"}`), util.NewLogger())
	require.Error(t, err)
	require.Contains(t, err.Error(), "The AniList API has been temporarily disabled due to severe stability issues.")
}

func newTestCacheLayer(t *testing.T, client *cacheLayerTestClient) *CacheLayer {
	t.Helper()
	ShouldCache.Store(true)
	IsWorking.Store(true)
	clearFailureTracking()

	clientRef := util.NewRef[anilist.AnilistClient](client)
	cacheLayer, ok := NewCacheLayer(clientRef).(*CacheLayer)
	require.True(t, ok)

	t.Cleanup(func() {
		ShouldCache.Store(true)
		IsWorking.Store(true)
		clearFailureTracking()
	})

	return cacheLayer
}

func newTestAnimeCollection(mediaID int, entryID int, status anilist.MediaListStatus, progress int) *anilist.AnimeCollection {
	return &anilist.AnimeCollection{
		MediaListCollection: &anilist.AnimeCollection_MediaListCollection{
			Lists: []*anilist.AnimeCollection_MediaListCollection_Lists{
				newTestAnimeList(status, newTestAnimeEntry(mediaID, entryID, status, progress)),
				newTestAnimeList(anilist.MediaListStatusCompleted),
			},
		},
	}
}

func newTestAnimeList(status anilist.MediaListStatus, entries ...*anilist.AnimeCollection_MediaListCollection_Lists_Entries) *anilist.AnimeCollection_MediaListCollection_Lists {
	return &anilist.AnimeCollection_MediaListCollection_Lists{
		Status:       new(status),
		Name:         new(string(status)),
		IsCustomList: new(false),
		Entries:      entries,
	}
}

func newTestAnimeEntry(mediaID int, entryID int, status anilist.MediaListStatus, progress int) *anilist.AnimeCollection_MediaListCollection_Lists_Entries {
	return &anilist.AnimeCollection_MediaListCollection_Lists_Entries{
		ID:       entryID,
		Media:    &anilist.BaseAnime{ID: mediaID, Episodes: new(12)},
		Status:   new(status),
		Progress: new(progress),
		Score:    new(0.0),
	}
}

func newTestMangaCollection(mediaID int, entryID int, status anilist.MediaListStatus, progress int) *anilist.MangaCollection {
	return &anilist.MangaCollection{
		MediaListCollection: &anilist.MangaCollection_MediaListCollection{
			Lists: []*anilist.MangaCollection_MediaListCollection_Lists{
				newTestMangaList(status, newTestMangaEntry(mediaID, entryID, status, progress)),
				newTestMangaList(anilist.MediaListStatusCompleted),
			},
		},
	}
}

func newTestMangaList(status anilist.MediaListStatus, entries ...*anilist.MangaCollection_MediaListCollection_Lists_Entries) *anilist.MangaCollection_MediaListCollection_Lists {
	return &anilist.MangaCollection_MediaListCollection_Lists{
		Status:       new(status),
		Name:         new(string(status)),
		IsCustomList: new(false),
		Entries:      entries,
	}
}

func newTestMangaEntry(mediaID int, entryID int, status anilist.MediaListStatus, progress int) *anilist.MangaCollection_MediaListCollection_Lists_Entries {
	return &anilist.MangaCollection_MediaListCollection_Lists_Entries{
		ID:       entryID,
		Media:    &anilist.BaseManga{ID: mediaID, Chapters: new(20)},
		Status:   new(status),
		Progress: new(progress),
		Score:    new(0.0),
	}
}

func getCachedAnimeCollection(t *testing.T, cacheLayer *CacheLayer) *anilist.AnimeCollection {
	t.Helper()
	var cached anilist.AnimeCollection
	found, err := cacheLayer.fileCacher.GetPerm(cacheLayer.buckets[AnimeCollectionBucket], cacheLayer.generateCacheKey("collection", nil), &cached)
	require.NoError(t, err)
	require.True(t, found)
	return &cached
}

func getCachedMangaCollection(t *testing.T, cacheLayer *CacheLayer) *anilist.MangaCollection {
	t.Helper()
	var cached anilist.MangaCollection
	found, err := cacheLayer.fileCacher.GetPerm(cacheLayer.buckets[MangaCollectionBucket], cacheLayer.generateCacheKey("collection", nil), &cached)
	require.NoError(t, err)
	require.True(t, found)
	return &cached
}

func getQueuedUpdate(t *testing.T, cacheLayer *CacheLayer, mediaID int) queuedMediaListUpdate {
	t.Helper()
	var queued queuedMediaListUpdate
	found, err := cacheLayer.fileCacher.GetPerm(cacheLayer.buckets[PendingMediaListUpdatesBucket], strconv.Itoa(mediaID), &queued)
	require.NoError(t, err)
	require.True(t, found)
	return queued
}

func requireNoQueuedUpdate(t *testing.T, cacheLayer *CacheLayer, mediaID int) {
	t.Helper()
	var queued queuedMediaListUpdate
	found, err := cacheLayer.fileCacher.GetPerm(cacheLayer.buckets[PendingMediaListUpdatesBucket], strconv.Itoa(mediaID), &queued)
	require.NoError(t, err)
	require.False(t, found)
}

func animeListContains(collection *anilist.AnimeCollection, status anilist.MediaListStatus, mediaID int) bool {
	for _, list := range collection.GetMediaListCollection().GetLists() {
		if list.GetStatus() == nil || *list.GetStatus() != status {
			continue
		}
		for _, entry := range list.GetEntries() {
			if entry.GetMedia().GetID() == mediaID {
				return true
			}
		}
	}
	return false
}

func mangaListContains(collection *anilist.MangaCollection, status anilist.MediaListStatus, mediaID int) bool {
	for _, list := range collection.GetMediaListCollection().GetLists() {
		if list.GetStatus() == nil || *list.GetStatus() != status {
			continue
		}
		for _, entry := range list.GetEntries() {
			if entry.GetMedia().GetID() == mediaID {
				return true
			}
		}
	}
	return false
}
