package scanner

import (
	"seanime/internal/api/anilist"
	"seanime/internal/api/metadata_provider"
	"seanime/internal/library/anime"
	"seanime/internal/platforms/platform"
	"seanime/internal/testmocks"
	"seanime/internal/util"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stubOfflineDatabase(t *testing.T, media ...*anime.NormalizedMedia) *int {
	t.Helper()
	calls := new(int)
	orig := fetchOfflineDatabase
	fetchOfflineDatabase = func(map[int]bool) ([]*anime.NormalizedMedia, error) {
		*calls++
		return media, nil
	}
	t.Cleanup(func() { fetchOfflineDatabase = orig })
	return calls
}

func newEmptyCollectionFetcher(t *testing.T, plat platform.Platform, enhanced bool) *MediaFetcher {
	t.Helper()
	mf, err := NewMediaFetcher(t.Context(), &MediaFetcherOptions{
		Enhanced:            enhanced,
		PlatformRef:         util.NewRef(plat),
		LocalFiles:          []*anime.LocalFile{},
		CompleteAnimeCache:  anilist.NewCompleteAnimeCache(),
		MetadataProviderRef: util.NewRef[metadata_provider.Provider](testmocks.NewFakeMetadataProviderBuilder().Build()),
		Logger:              util.NewLogger(),
	})
	require.NoError(t, err)
	return mf
}

// A profile with no AniList account (or an empty list) used to fail every
// collection-only scan with "[matcher] no media fed into the matcher".
func TestMediaFetcherFallsBackToOfflineDatabaseWhenCollectionIsEmpty(t *testing.T) {
	offline := anime.NewNormalizedMedia(testmocks.NewBaseAnime(21, "One Piece"))
	calls := stubOfflineDatabase(t, offline)

	// FakePlatform returns a nil collection, like an unlinked AniList platform.
	mf := newEmptyCollectionFetcher(t, testmocks.NewFakePlatformBuilder().Build(), false)

	assert.Equal(t, 1, *calls)
	require.Len(t, mf.AllMedia, 1)
	assert.Equal(t, 21, mf.AllMedia[0].ID)
}

func TestMediaFetcherKeepsCollectionOnlyMatchingWhenCollectionHasEntries(t *testing.T) {
	calls := stubOfflineDatabase(t)

	collection := &anilist.AnimeCollectionWithRelations{
		MediaListCollection: &anilist.AnimeCollectionWithRelations_MediaListCollection{
			Lists: []*anilist.AnimeCollectionWithRelations_MediaListCollection_Lists{{
				Entries: []*anilist.AnimeCollectionWithRelations_MediaListCollection_Lists_Entries{{
					Media: &anilist.CompleteAnime{ID: 21},
				}},
			}},
		},
	}
	plat := testmocks.NewFakePlatformBuilder().WithAnimeCollectionWithRelations(collection).Build()

	mf := newEmptyCollectionFetcher(t, plat, false)

	assert.Zero(t, *calls, "offline database is only a fallback for an empty collection")
	require.Len(t, mf.AllMedia, 1)
	assert.Equal(t, 21, mf.AllMedia[0].ID)
}
