package shared_platform

import (
	"seanime/internal/api/anilist"
	"seanime/internal/util/diskstore"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/goccy/go-json"
	"github.com/rs/zerolog"
)

// TitleCache is the server-wide store of AniList title records. Records hold only public data, so
// every profile shares them, and bulk results fill it for later single-title lookups.
type TitleCache struct {
	store  *diskstore.Store
	logger *zerolog.Logger
	now    func() time.Time
}

type titleEntry[M any] struct {
	FetchedAt int64 `json:"fetchedAt"`
	Media     *M    `json:"media"`
}

func NewTitleCache(dir string, maxBytes int64, logger *zerolog.Logger) (*TitleCache, error) {
	store, err := diskstore.New(dir, func() int64 { return maxBytes }, logger)
	if err != nil {
		return nil, err
	}
	return &TitleCache{store: store, logger: logger, now: time.Now}, nil
}

var titleCache atomic.Pointer[TitleCache]

// SetTitleCache installs the title cache every CacheLayer shares.
func SetTitleCache(tc *TitleCache) {
	titleCache.Store(tc)
}

// CurrentTitleCache returns the installed title cache, or nil when none is installed.
func CurrentTitleCache() *TitleCache {
	return titleCache.Load()
}

func (tc *TitleCache) GetAnime(id int) (*anilist.BaseAnime, bool, bool) {
	return getTitle[anilist.BaseAnime](tc, "anime", id)
}

func (tc *TitleCache) GetManga(id int) (*anilist.BaseManga, bool, bool) {
	return getTitle[anilist.BaseManga](tc, "manga", id)
}

func (tc *TitleCache) PutAnime(media ...*anilist.BaseAnime) {
	for _, m := range media {
		if m != nil {
			putTitle(tc, "anime", m.ID, m)
		}
	}
}

func (tc *TitleCache) PutManga(media ...*anilist.BaseManga) {
	for _, m := range media {
		if m != nil {
			putTitle(tc, "manga", m.ID, m)
		}
	}
}

func (tc *TitleCache) Size() int64 {
	if tc == nil {
		return 0
	}
	return tc.store.Size()
}

func (tc *TitleCache) Clear() error {
	if tc == nil {
		return nil
	}
	return tc.store.Clear()
}

func getTitle[M any](tc *TitleCache, kind string, id int) (media *M, fresh bool, ok bool) {
	if tc == nil {
		return nil, false, false
	}
	data, found := tc.store.Get(titleKey(kind, id))
	if !found {
		return nil, false, false
	}
	var entry titleEntry[M]
	if err := json.Unmarshal(data, &entry); err != nil || entry.Media == nil {
		return nil, false, false
	}
	return entry.Media, tc.now().Sub(time.Unix(entry.FetchedAt, 0)) < longCacheTTL, true
}

func putTitle[M any](tc *TitleCache, kind string, id int, media *M) {
	// While AniList is down, bulk results may be old stored copies that must not be saved as fresh.
	if tc == nil || id == 0 || !ShouldCache.Load() || !IsWorking.Load() {
		return
	}
	data, err := json.Marshal(titleEntry[M]{FetchedAt: tc.now().Unix(), Media: media})
	if err == nil {
		err = tc.store.Put(titleKey(kind, id), data)
	}
	if err != nil {
		tc.logger.Warn().Err(err).Int("mediaID", id).Msg("anilist cache: Failed to save title")
	}
}

// The kind is part of the key so a lookup never decodes one type's record as the other.
func titleKey(kind string, id int) string {
	return kind + ":" + strconv.Itoa(id)
}
