package torrentstream

import (
	"seanime/internal/api/anilist"
	"seanime/internal/util"
	"testing"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
)

// TestGetActiveStreamInfoAbsentWithoutCurrentTorrent guards the "nothing playing" case:
// an admin activity snapshot must not show a row for a profile whose session exists but
// isn't actively streaming anything.
func TestGetActiveStreamInfoAbsentWithoutCurrentTorrent(t *testing.T) {
	repo := &Repository{logger: util.NewLogger()}
	c := NewClient(repo)
	repo.client = c
	t.Cleanup(func() { unregisterClient(c) })

	_, _, ok := c.GetActiveStreamInfo()
	require.False(t, ok)

	info, ok := repo.GetActiveStreamInfo()
	require.False(t, ok)
	require.Equal(t, ActiveStreamInfo{}, info)
}

// TestGetActiveStreamInfoResolvesTitleFromCache guards the admin activity view's display
// name: when the profile's previously-started stream options reference a media ID present
// in the shared base anime cache, the resolved Title must come from that cache rather than
// falling back to the raw torrent filename.
func TestGetActiveStreamInfoResolvesTitleFromCache(t *testing.T) {
	const (
		pieceLen = int64(1 << 20)
		pieces   = 4
	)
	infoBytes, err := bencode.Marshal(metainfo.Info{
		Name:        t.Name() + ".mkv",
		Length:      pieceLen * pieces,
		PieceLength: pieceLen,
		Pieces:      make([]byte, metainfo.HashSize*pieces),
	})
	require.NoError(t, err)

	cfg := torrent.TestingConfig(t)
	cfg.DisableTCP = true
	cfg.DisableUTP = true
	tc, err := torrent.NewClient(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { tc.Close() })

	tor, err := tc.AddTorrent(&metainfo.MetaInfo{InfoBytes: infoBytes})
	require.NoError(t, err)

	romaji := "Test Anime"
	cache := anilist.NewBaseAnimeCache()
	cache.Set(123, &anilist.BaseAnime{ID: 123, Title: &anilist.BaseAnime_Title{Romaji: &romaji}})

	repo := &Repository{logger: util.NewLogger(), baseAnimeCache: cache}
	repo.previousStreamOptions = mo.Some(&StartStreamOptions{MediaId: 123, EpisodeNumber: 4})

	c := NewClient(repo)
	repo.client = c
	t.Cleanup(func() { unregisterClient(c) })
	c.torrentClient = mo.Some(tc)
	c.currentTorrent = mo.Some(tor)
	c.currentTorrentStatus = TorrentStatus{ProgressPercentage: 42, Seeders: 3}

	name, status, ok := c.GetActiveStreamInfo()
	require.True(t, ok)
	require.Equal(t, tor.Name(), name)
	require.Equal(t, float64(42), status.ProgressPercentage)

	info, ok := repo.GetActiveStreamInfo()
	require.True(t, ok)
	require.Equal(t, 123, info.MediaID)
	require.Equal(t, 4, info.EpisodeNumber)
	require.Equal(t, "Test Anime", info.Title)
	require.Equal(t, float64(3), float64(info.Status.Seeders))
}
