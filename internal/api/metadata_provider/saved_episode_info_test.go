package metadata_provider

import (
	"errors"
	"seanime/internal/api/metadata"
	"seanime/internal/util"
	"seanime/internal/util/diskstore"
	"seanime/internal/util/result"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/singleflight"
)

func newSavingProvider(t *testing.T, fetch func(metadata.Platform, int) (*metadata.AnimeMetadata, error)) (*ProviderImpl, *diskstore.Store) {
	t.Helper()
	store, err := diskstore.New(t.TempDir(), func() int64 { return 1 << 20 }, util.NewLogger())
	require.NoError(t, err)
	p := &ProviderImpl{
		logger:             util.NewLogger(),
		animeMetadataCache: result.NewBoundedCache[string, *metadata.AnimeMetadata](100),
		singleflight:       &singleflight.Group{},
		episodeInfo:        store,
		fetch:              fetch,
	}
	return p, store
}

func showMetadata(title string) *metadata.AnimeMetadata {
	return &metadata.AnimeMetadata{
		Titles:   map[string]string{"en": title},
		Episodes: map[string]*metadata.EpisodeMetadata{"1": {Title: "Pilot", Episode: "1"}},
	}
}

// TestSavedEpisodeInfoServesDuringOutage guards episode lists during an outage: a failed fetch
// must fall back to the last saved copy.
func TestSavedEpisodeInfoServesDuringOutage(t *testing.T) {
	online := true
	p, _ := newSavingProvider(t, func(metadata.Platform, int) (*metadata.AnimeMetadata, error) {
		if online {
			return showMetadata("Show"), nil
		}
		return nil, errors.New("network down")
	})

	_, err := p.GetAnimeMetadata(metadata.AnilistPlatform, 1)
	require.NoError(t, err)

	online = false
	p.animeMetadataCache.Clear() // as after a restart
	got, err := p.GetAnimeMetadata(metadata.AnilistPlatform, 1)
	require.NoError(t, err)
	require.Equal(t, "Show", got.Titles["en"])
	require.Equal(t, "Pilot", got.Episodes["1"].Title)
}

func TestSuccessfulFetchOverwritesSavedCopy(t *testing.T) {
	title := "Old"
	online := true
	p, _ := newSavingProvider(t, func(metadata.Platform, int) (*metadata.AnimeMetadata, error) {
		if online {
			return showMetadata(title), nil
		}
		return nil, errors.New("network down")
	})

	_, _ = p.GetAnimeMetadata(metadata.AnilistPlatform, 1)
	title = "New"
	p.animeMetadataCache.Clear()
	_, _ = p.GetAnimeMetadata(metadata.AnilistPlatform, 1)

	online = false
	p.animeMetadataCache.Clear()
	got, err := p.GetAnimeMetadata(metadata.AnilistPlatform, 1)
	require.NoError(t, err)
	require.Equal(t, "New", got.Titles["en"])
}

func TestNoSavedCopyReturnsOriginalError(t *testing.T) {
	down := errors.New("network down")
	p, _ := newSavingProvider(t, func(metadata.Platform, int) (*metadata.AnimeMetadata, error) {
		return nil, down
	})

	_, err := p.GetAnimeMetadata(metadata.AnilistPlatform, 1)
	require.ErrorIs(t, err, down)
}

// Review focus 5: a fetch that returns no data and no error saves nothing and doesn't crash.
func TestEmptyFetchIsNotSaved(t *testing.T) {
	p, store := newSavingProvider(t, func(metadata.Platform, int) (*metadata.AnimeMetadata, error) {
		return nil, nil
	})

	got, err := p.GetAnimeMetadata(metadata.AnilistPlatform, 1)
	require.NoError(t, err)
	require.Nil(t, got)
	require.EqualValues(t, 0, store.Size())
}
