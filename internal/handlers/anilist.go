package handlers

import (
	"context"
	"errors"
	"fmt"
	"seanime/internal/api/anilist"
	"seanime/internal/api/simkl"
	"seanime/internal/core"
	"seanime/internal/platforms/shared_platform"
	syncpkg "seanime/internal/sync"
	"seanime/internal/util/result"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// HandleGetAnimeCollection
//
//	@summary returns the user's AniList anime collection.
//	@desc Calling GET will return the cached anime collection.
//	@desc The manga collection is also refreshed in the background, and upon completion, a WebSocket event is sent.
//	@desc Calling POST will refetch both the anime and manga collections.
//	@returns anilist.AnimeCollection
//	@route /api/v1/anilist/collection [GET,POST]
func (h *Handler) HandleGetAnimeCollection(c echo.Context) error {

	bypassCache := c.Request().Method == "POST"
	plat := h.getAnilistPlatform(c)

	animeCollection, err := plat.GetAnimeCollection(c.Request().Context(), bypassCache)
	if err != nil || animeCollection == nil {
		animeCollection = &anilist.AnimeCollection{
			MediaListCollection: &anilist.AnimeCollection_MediaListCollection{
				Lists: []*anilist.AnimeCollection_MediaListCollection_Lists{},
			},
		}
	}

	if bypassCache {
		go func() {
			_, _ = plat.RefreshMangaCollection(c.Request().Context())
		}()
	}

	return h.RespondWithData(c, animeCollection)
}

// HandleGetRawAnimeCollection
//
//	@summary returns the user's AniList anime collection without filtering out custom lists.
//	@desc Calling GET will return the cached anime collection.
//	@returns anilist.AnimeCollection
//	@route /api/v1/anilist/collection/raw [GET,POST]
func (h *Handler) HandleGetRawAnimeCollection(c echo.Context) error {

	bypassCache := c.Request().Method == "POST"

	// Get the user's anilist collection
	plat := h.getAnilistPlatform(c)
	animeCollection, err := plat.GetRawAnimeCollection(c.Request().Context(), bypassCache)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	return h.RespondWithData(c, animeCollection)
}

// tagsCache is keyed by profileID ("" in single-user mode) so tags fetched
// for one profile's AniList account are never served back to another.
var tagsCache = result.NewMap[string, anilist.MediaTagMap]()

// HandleGetRawAnimeCollectionTags
//
//	@summary returns the AniList tags for the user's raw anime collection.
//	@desc This runs a dedicated AniList tags query used by the lists page filters.
//	@returns anilist.MediaTagMap
//	@route /api/v1/anilist/collection/raw/tags [GET]
func (h *Handler) HandleGetRawAnimeCollectionTags(c echo.Context) error {
	h.App.OnRefreshAnilistCollectionFuncs.Set("HandleGetRawAnimeCollectionTags", func() {
		tagsCache.Clear()
	})

	profileID := ""
	if h.App.MultiUserEnabled {
		profileID = core.GetProfileIDFromContext(c)
	}

	if tags, found := tagsCache.Get(profileID); found {
		return h.RespondWithData(c, tags)
	}

	var userName string
	if h.App.MultiUserEnabled && profileID != "" {
		acc, _ := h.App.Database.GetAccountByProfileID(profileID)
		if acc == nil || acc.Token == "" || acc.Username == "" {
			return h.RespondWithData(c, anilist.MediaTagMap{})
		}
		userName = acc.Username
	} else {
		userName = h.App.GetUsername()
		if userName == "" || h.App.GetUser().IsSimulated {
			return h.RespondWithData(c, anilist.MediaTagMap{})
		}
	}

	ret, err := h.getAnilistPlatform(c).GetAnilistClient().AnimeCollectionTags(c.Request().Context(), &userName)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	tags := anilist.MediaTagMapFromAnimeCollectionTags(ret)
	tagsCache.Set(profileID, tags)

	return h.RespondWithData(c, tags)
}

// HandleEditAnilistListEntry
//
//	@summary updates the user's list entry on Anilist.
//	@desc This is used to edit an entry on AniList.
//	@desc The "type" field is used to determine if the entry is an anime or manga and refreshes the collection accordingly.
//	@desc The client should refetch collection-dependent queries after this mutation.
//	@returns true
//	@route /api/v1/anilist/list-entry [POST]
func (h *Handler) HandleEditAnilistListEntry(c echo.Context) error {

	type body struct {
		MediaId   *int                     `json:"mediaId"`
		Status    *anilist.MediaListStatus `json:"status"`
		Score     *int                     `json:"score"`
		Progress  *int                     `json:"progress"`
		StartDate *anilist.FuzzyDateInput  `json:"startedAt"`
		EndDate   *anilist.FuzzyDateInput  `json:"completedAt"`
		Type      string                   `json:"type"`
	}

	p := new(body)
	if err := c.Bind(p); err != nil {
		return h.RespondWithError(c, err)
	}

	ctx := c.Request().Context()
	if p.Type == "manga" {
		// SIMKL only tracks anime - without this marker a manga edit would be mirrored to
		// SIMKL's anime endpoints as if it were an anime entry. See syncpkg.WithMangaMedia.
		ctx = syncpkg.WithMangaMedia(ctx)
	}

	err := h.getAnilistPlatform(c).UpdateEntry(
		ctx,
		*p.MediaId,
		p.Status,
		p.Score,
		p.Progress,
		p.StartDate,
		p.EndDate,
	)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	switch p.Type {
	case "anime":
		_, _ = h.getAnilistPlatform(c).RefreshAnimeCollection(c.Request().Context())
	case "manga":
		_, _ = h.getAnilistPlatform(c).RefreshMangaCollection(c.Request().Context())
	default:
		_, _ = h.getAnilistPlatform(c).RefreshAnimeCollection(c.Request().Context())
		_, _ = h.getAnilistPlatform(c).RefreshMangaCollection(c.Request().Context())
	}

	return h.RespondWithData(c, true)
}

//----------------------------------------------------------------------------------------------------------------------------------------------------

var (
	detailsCache = result.NewCache[int, *anilist.AnimeDetailsById_Media]()
)

// HandleGetAnilistAnimeDetails
//
//	@summary returns more details about an AniList anime entry.
//	@desc This fetches more fields omitted from the base queries.
//	@param id - int - true - "The AniList anime ID"
//	@returns anilist.AnimeDetailsById_Media
//	@route /api/v1/anilist/media-details/{id} [GET]
func (h *Handler) HandleGetAnilistAnimeDetails(c echo.Context) error {

	mId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return h.RespondWithError(c, err)
	}

	// The forced-testing override (Settings > App > Metadata Providers) bypasses the cache read
	// AND write: otherwise a previously-cached real AniList result would keep being served (never
	// even calling the platform again) for the whole TTL, defeating the point of forcing a fresh
	// SIMKL-fallback attempt - see FallbackPlatform.GetAnimeDetails for where the force actually
	// engages the SIMKL call. Gated on DiscoveryAvailable too (checked only if the cheap atomic is
	// already true, same as forcedSimklClient) - with the override on but no client_id configured,
	// FallbackPlatform can't do anything with it anyway, so there's no reason to also disable
	// caching in that state.
	forced := shared_platform.ForceSimklFallback.Load()
	if forced {
		if simklClient, ok := h.simklClientForProfile(c); !ok || !syncpkg.DiscoveryAvailable(simklClient.ClientID()) {
			forced = false
		}
	}

	if !forced {
		if details, ok := detailsCache.Get(mId); ok {
			return h.RespondWithData(c, details)
		}
	}
	details, err := h.getAnilistPlatform(c).GetAnimeDetails(c.Request().Context(), mId)
	if err != nil {
		return h.RespondWithError(c, err)
	}
	if shared_platform.IsWorking.Load() && !forced {
		detailsCache.Set(mId, details)
	}

	return h.RespondWithData(c, details)
}

//----------------------------------------------------------------------------------------------------------------------------------------------------

var studioDetailsMap = result.NewMap[int, *anilist.StudioDetails]()

// HandleGetAnilistStudioDetails
//
//	@summary returns details about a studio.
//	@desc This fetches media produced by the studio.
//	@param id - int - true - "The AniList studio ID"
//	@returns anilist.StudioDetails
//	@route /api/v1/anilist/studio-details/{id} [GET]
func (h *Handler) HandleGetAnilistStudioDetails(c echo.Context) error {

	mId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return h.RespondWithError(c, err)
	}

	if details, ok := studioDetailsMap.Get(mId); ok {
		return h.RespondWithData(c, details)
	}
	details, err := h.getAnilistPlatform(c).GetStudioDetails(c.Request().Context(), mId)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	go func() {
		if details != nil {
			studioDetailsMap.Set(mId, details)
		}
	}()

	return h.RespondWithData(c, details)
}

//----------------------------------------------------------------------------------------------------------------------------------------------------

// HandleDeleteAnilistListEntry
//
//	@summary deletes an entry from the user's AniList list.
//	@desc This is used to delete an entry on AniList.
//	@desc The "type" field is used to determine if the entry is an anime or manga and refreshes the collection accordingly.
//	@desc The client should refetch collection-dependent queries after this mutation.
//	@route /api/v1/anilist/list-entry [DELETE]
//	@returns bool
func (h *Handler) HandleDeleteAnilistListEntry(c echo.Context) error {

	type body struct {
		MediaId *int    `json:"mediaId"`
		Type    *string `json:"type"`
	}

	p := new(body)
	if err := c.Bind(p); err != nil {
		return h.RespondWithError(c, err)
	}

	if p.Type == nil || p.MediaId == nil {
		return h.RespondWithError(c, errors.New("missing parameters"))
	}

	var listEntryID int

	switch *p.Type {
	case "anime":
		// Get the list entry ID
		animeCollection, err := h.getAnilistPlatform(c).GetAnimeCollection(c.Request().Context(), false)
		if err != nil {
			return h.RespondWithError(c, err)
		}

		listEntry, found := animeCollection.GetListEntryFromAnimeId(*p.MediaId)
		if !found {
			return h.RespondWithError(c, errors.New("list entry not found"))
		}
		listEntryID = listEntry.ID
	case "manga":
		// Get the list entry ID
		mangaCollection, err := h.getAnilistPlatform(c).GetMangaCollection(c.Request().Context(), false)
		if err != nil {
			return h.RespondWithError(c, err)
		}

		listEntry, found := mangaCollection.GetListEntryFromMangaId(*p.MediaId)
		if !found {
			return h.RespondWithError(c, errors.New("list entry not found"))
		}
		listEntryID = listEntry.ID
	}

	// Delete the list entry
	ctx := c.Request().Context()
	if *p.Type == "manga" {
		// See the matching comment in HandleEditAnilistListEntry - SIMKL only tracks anime.
		ctx = syncpkg.WithMangaMedia(ctx)
	}
	err := h.getAnilistPlatform(c).DeleteEntry(ctx, *p.MediaId, listEntryID)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	switch *p.Type {
	case "anime":
		_, _ = h.getAnilistPlatform(c).RefreshAnimeCollection(c.Request().Context())
	case "manga":
		_, _ = h.getAnilistPlatform(c).RefreshMangaCollection(c.Request().Context())
	}

	return h.RespondWithData(c, true)
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

var (
	anilistListAnimeCache       = result.NewCache[string, *anilist.ListAnime]()
	anilistListRecentAnimeCache = result.NewCache[string, *anilist.ListRecentAnime]() // holds 1 value
	anilistListSeasonAnimeCache = result.NewCache[string, []*anilist.BaseAnime]()
)

// shouldTrySimklDiscoveryFallback reports whether a Discover/Search/Calendar/Details fallback
// call should be attempted: AniList must be known down AND the profile must have a SIMKL
// client_id configured. This is intentionally lighter than Component 1's tracking-fallback gate
// (FallbackPlatform.simklAvailable) - see the spec's "revised gating" note: no OAuth/AccessToken/
// Enabled requirement, just the client_id.
func shouldTrySimklDiscoveryFallback(simklClientID string) bool {
	return !shared_platform.IsWorking.Load() && syncpkg.DiscoveryAvailable(simklClientID)
}

// simklClientForProfile builds a SIMKL API client for discovery-fallback calls (search,
// trending, calendar, details) using only the profile's configured client_id - no access token
// needed for these endpoints, unlike Component 1's per-profile sync client.
func (h *Handler) simklClientForProfile(c echo.Context) (*simkl.APIClient, bool) {
	profileID := core.NormalizeSimklProfileID(core.GetProfileIDFromContext(c))
	settings, err := h.App.Database.GetSimklSettings(profileID)
	if err != nil || settings.ClientId == "" {
		return nil, false
	}
	return simkl.NewAPIClient(simkl.DefaultHTTPClient, "", settings.ClientId), true
}

// shouldForceSimklFallback reports whether the manual "force SIMKL fallback" testing override
// (Settings > App > Metadata Providers) should engage: the override must be on AND the profile
// must have a SIMKL client_id configured. Mirrors shouldTrySimklDiscoveryFallback's shape, but is
// checked BEFORE attempting the real AniList call rather than in its error branch - forcing is
// meant to skip waiting on AniList entirely, not just override its result after the fact.
func shouldForceSimklFallback(simklClientID string) bool {
	return shared_platform.ForceSimklFallback.Load() && syncpkg.DiscoveryAvailable(simklClientID)
}

// forcedSimklClient returns a usable SIMKL client only when shouldForceSimklFallback is true for
// this profile - lets a user verify the Discover/Search/Schedule fallback UI without needing
// AniList to actually be down.
func (h *Handler) forcedSimklClient(c echo.Context) (*simkl.APIClient, bool) {
	if !shared_platform.ForceSimklFallback.Load() {
		return nil, false // cheap check first, avoids a DB read on every request when the override is off
	}
	simklClient, ok := h.simklClientForProfile(c)
	if !ok || !shouldForceSimklFallback(simklClient.ClientID()) {
		return nil, false
	}
	return simklClient, true
}

// simklDiscoveryEnrichmentCache resolves SIMKL ids to AniList-mapped detail records for
// discovery-fallback list results (see internal/sync/id_resolution_cache.go) - package-level and
// shared across requests since the resolved crosswalk itself is stable regardless of which
// profile's client made the call. The resolver function is NOT stored here - see
// IDResolutionCache's doc comment: it is passed fresh to each ResolveMany call so two profiles'
// concurrent requests never race onto each other's SIMKL client.
var simklDiscoveryEnrichmentCache = syncpkg.NewIDResolutionCache(8, 24*time.Hour)

const simklDiscoveryEnrichmentCap = 20

// isUpcomingOnlyStatus reports whether a list-anime request is Discover's "Coming Soon" section
// specifically - the only caller that requests exactly NOT_YET_RELEASED and nothing else
// (useDiscoverUpcomingAnime sends status: ["NOT_YET_RELEASED"], no other status alongside it).
// Deliberately narrow rather than "contains NOT_YET_RELEASED anywhere": a broader multi-status
// query has no single SIMKL equivalent to fall back to.
func isUpcomingOnlyStatus(status []*anilist.MediaStatus) bool {
	return len(status) == 1 && status[0] != nil && *status[0] == anilist.MediaStatusNotYetReleased
}

// simklDiscoveryFallback handles Discover's trending section, Search's text-query fallback, and
// Discover's "Coming Soon" section (see spec Component 2). Before this added the "Coming Soon"
// branch, a NOT_YET_RELEASED-only request silently fell into the trending branch instead - wrong
// data, not a crash, so it went unnoticed until checked directly. page/perPage/sort filtering
// beyond these three cases is not replicated - SIMKL has no equivalent server-side filters - so
// this returns a best-effort unfiltered result set capped at simklDiscoveryEnrichmentCap items.
func simklDiscoveryFallback(ctx context.Context, simklClient *simkl.APIClient, search *string, status []*anilist.MediaStatus) (*anilist.ListAnime, error) {
	var media []*anilist.BaseAnime
	switch {
	case search != nil && *search != "":
		results, err := simklClient.SearchAnime(ctx, *search)
		if err != nil {
			return nil, err
		}
		ids := make([]int, len(results))
		for i, r := range results {
			ids[i] = r.Ids.SimklID
		}
		// GetAnimeDetails already matches syncpkg.IDResolver's signature exactly - no wrapper
		// closure needed.
		resolved := simklDiscoveryEnrichmentCache.ResolveMany(ctx, simklClient.GetAnimeDetails, ids, simklDiscoveryEnrichmentCap)
		media = syncpkg.MapSearchResultsToBaseAnime(results, resolved)
	case isUpcomingOnlyStatus(status):
		entries, err := simklClient.GetUpcomingAnime(ctx)
		if err != nil {
			return nil, err
		}
		ids := make([]int, len(entries))
		for i, e := range entries {
			ids[i] = e.Ids.SimklID
		}
		resolved := simklDiscoveryEnrichmentCache.ResolveMany(ctx, simklClient.GetAnimeDetails, ids, simklDiscoveryEnrichmentCap)
		media = syncpkg.MapUpcomingToBaseAnime(entries, resolved)
	default:
		entries, err := simklClient.GetTrendingAnime(ctx)
		if err != nil {
			return nil, err
		}
		ids := make([]int, len(entries))
		for i, e := range entries {
			ids[i] = e.Ids.SimklID
		}
		resolved := simklDiscoveryEnrichmentCache.ResolveMany(ctx, simklClient.GetAnimeDetails, ids, simklDiscoveryEnrichmentCap)
		media = syncpkg.MapTrendingToBaseAnime(entries, resolved)
	}

	total := len(media)
	return &anilist.ListAnime{
		Page: &anilist.ListAnime_Page{
			Media: media,
			PageInfo: &anilist.ListAnime_Page_PageInfo{
				Total: &total,
			},
		},
	}, nil
}

var simklCalendarEntriesCache = result.NewCache[string, []simkl.CalendarEntry]()

const simklCalendarCacheKey = "calendar"

// getCalendarEntriesCached wraps GetAnimeCalendar with a short TTL - the feed itself doesn't vary
// per profile/client_id, and every simklCalendarFallback*/HandleAnilistList* caller during a single
// outage would otherwise re-download the same ~658-row feed on every request.
func getCalendarEntriesCached(ctx context.Context, simklClient *simkl.APIClient) ([]simkl.CalendarEntry, error) {
	if entries, ok := simklCalendarEntriesCache.Get(simklCalendarCacheKey); ok {
		return entries, nil
	}
	entries, err := simklClient.GetAnimeCalendar(ctx)
	if err != nil {
		return nil, err
	}
	simklCalendarEntriesCache.SetT(simklCalendarCacheKey, entries, 5*time.Minute)
	return entries, nil
}

// dedupeSimklIDs collects each entry's SimklID once, preserving first-seen order. The calendar
// feed has one row per episode, so deduping before capping at simklDiscoveryEnrichmentCap avoids
// wasting resolve slots on repeated concurrent lookups of the same show.
func dedupeSimklIDs(entries []simkl.CalendarEntry) []int {
	seen := make(map[int]bool, len(entries))
	ids := make([]int, 0, len(entries))
	for _, e := range entries {
		if seen[e.SimklID] {
			continue
		}
		seen[e.SimklID] = true
		ids = append(ids, e.SimklID)
	}
	return ids
}

// simklCalendarFallback handles Component 3 (Schedule page fallback) for both
// HandleAnilistListSeasonAnime and HandleAnilistListRecentAiringAnime. Both return a flat
// []*anilist.BaseAnime-compatible shape, so one helper serves both call sites - the "This Season"
// tab gets a reduced (rolling-window) result compared to its normal full-season query, and the
// recent/upcoming airing section is a closer fit since it's already window-based in its normal
// AniList-backed form (see spec Component 3).
func simklCalendarFallback(ctx context.Context, simklClient *simkl.APIClient) ([]*anilist.BaseAnime, error) {
	entries, err := getCalendarEntriesCached(ctx, simklClient)
	if err != nil {
		return nil, err
	}

	ids := dedupeSimklIDs(entries)
	resolved := simklDiscoveryEnrichmentCache.ResolveMany(ctx, simklClient.GetAnimeDetails, ids, simklDiscoveryEnrichmentCap)

	return syncpkg.MapCalendarToBaseAnime(entries, resolved), nil
}

// simklCalendarFallbackSchedules is simklCalendarFallback's counterpart for
// HandleAnilistListRecentAiringAnime, which needs ListRecentAnime_Page_AiringSchedules entries
// (with per-entry airingAt/episode) rather than bare BaseAnime. airingAtGreater/airingAtLesser
// mirror the caller's own request window (see FilterAiringSchedulesByWindow) so the fallback
// doesn't always return the full unfiltered calendar feed.
func simklCalendarFallbackSchedules(ctx context.Context, simklClient *simkl.APIClient, airingAtGreater, airingAtLesser *int) ([]*anilist.ListRecentAnime_Page_AiringSchedules, error) {
	entries, err := getCalendarEntriesCached(ctx, simklClient)
	if err != nil {
		return nil, err
	}

	ids := dedupeSimklIDs(entries)
	resolved := simklDiscoveryEnrichmentCache.ResolveMany(ctx, simklClient.GetAnimeDetails, ids, simklDiscoveryEnrichmentCap)

	schedules := syncpkg.MapCalendarToAiringSchedules(entries, resolved)
	return syncpkg.FilterAiringSchedulesByWindow(schedules, airingAtGreater, airingAtLesser), nil
}

// HandleAnilistListAnime
//
//	@summary returns a list of anime based on the search parameters.
//	@desc This is used by the "Discover" and "Advanced Search".
//	@route /api/v1/anilist/list-anime [POST]
//	@returns anilist.ListAnime
func (h *Handler) HandleAnilistListAnime(c echo.Context) error {

	type body struct {
		Page                *int                   `json:"page,omitempty"`
		Search              *string                `json:"search,omitempty"`
		PerPage             *int                   `json:"perPage,omitempty"`
		Sort                []*anilist.MediaSort   `json:"sort,omitempty"`
		Status              []*anilist.MediaStatus `json:"status,omitempty"`
		Genres              []*string              `json:"genres,omitempty"`
		Tags                []*string              `json:"tags,omitempty"`
		AverageScoreGreater *int                   `json:"averageScore_greater,omitempty"`
		Season              *anilist.MediaSeason   `json:"season,omitempty"`
		SeasonYear          *int                   `json:"seasonYear,omitempty"`
		Format              *anilist.MediaFormat   `json:"format,omitempty"`
		IsAdult             *bool                  `json:"isAdult,omitempty"`
		CountryOfOrigin     *string                `json:"countryOfOrigin,omitempty"`
	}

	p := new(body)
	if err := c.Bind(p); err != nil {
		return h.RespondWithError(c, err)
	}

	if p.Page == nil || p.PerPage == nil {
		*p.Page = 1
		*p.PerPage = 20
	}

	var isAdult *bool = nil
	if p.IsAdult != nil {
		enableAdult := false
		if currentSettings, settingsErr := h.getSettings(c); settingsErr == nil && currentSettings.GetAnilist() != nil {
			enableAdult = currentSettings.GetAnilist().EnableAdultContent
		}
		val := *p.IsAdult && enableAdult
		isAdult = &val
	}

	// Checked before the cache lookup too, so a forced test always exercises a fresh SIMKL call
	// rather than serving a previously-cached real AniList result.
	if simklClient, ok := h.forcedSimklClient(c); ok {
		if fallback, fbErr := simklDiscoveryFallback(c.Request().Context(), simklClient, p.Search, p.Status); fbErr == nil {
			return h.RespondWithData(c, fallback)
		}
	}

	cacheKey := anilist.ListAnimeCacheKey(
		p.Page,
		p.Search,
		p.PerPage,
		p.Sort,
		p.Status,
		p.Genres,
		p.Tags,
		p.AverageScoreGreater,
		p.Season,
		p.SeasonYear,
		p.Format,
		isAdult,
		p.CountryOfOrigin,
	)

	cached, ok := anilistListAnimeCache.Get(cacheKey)
	if ok {
		return h.RespondWithData(c, cached)
	}

	ret, err := anilist.ListAnimeM(
		h.App.AnilistPlatformRef.Get().GetAnilistClient(),
		p.Page,
		p.Search,
		p.PerPage,
		p.Sort,
		p.Status,
		p.Genres,
		p.Tags,
		p.AverageScoreGreater,
		p.Season,
		p.SeasonYear,
		p.Format,
		isAdult,
		p.CountryOfOrigin,
		h.App.Logger,
		h.App.GetUserAnilistToken(),
	)
	if err != nil {
		if simklClient, ok := h.simklClientForProfile(c); ok && shouldTrySimklDiscoveryFallback(simklClient.ClientID()) {
			if fallback, fbErr := simklDiscoveryFallback(c.Request().Context(), simklClient, p.Search, p.Status); fbErr == nil {
				return h.RespondWithData(c, fallback)
			}
		}
		return h.RespondWithError(c, err)
	}

	if ret != nil {
		anilistListAnimeCache.SetT(cacheKey, ret, time.Minute*10)
	}

	return h.RespondWithData(c, ret)
}

// HandleAnilistListSeasonAnime
//
//	@summary returns all anime in a given AniList season, aggregated across pages.
//	@desc Used by the schedule page's "This Season" tab. Loops AniList's ListAnime query
//	@desc server-side until the season is exhausted and returns a flat array of anime.
//	@desc Per-profile adult content filtering is applied: if the profile has
//	@desc EnableAdultContent=false, adult entries are excluded; if true, both adult and
//	@desc non-adult entries are returned (isAdult filter is omitted from the AniList query).
//	@route /api/v1/anilist/season-anime [POST]
//	@returns []anilist.BaseAnime
func (h *Handler) HandleAnilistListSeasonAnime(c echo.Context) error {

	type body struct {
		Season     *anilist.MediaSeason `json:"season"`
		SeasonYear *int                 `json:"seasonYear"`
		Sort       []*anilist.MediaSort `json:"sort"`
	}

	p := new(body)
	if err := c.Bind(p); err != nil {
		return h.RespondWithError(c, err)
	}

	if p.Season == nil || p.SeasonYear == nil {
		return h.RespondWithError(c, errors.New("season and seasonYear are required"))
	}

	// Per-profile adult content handling: if disabled, force isAdult=false so the AniList
	// query excludes adult entries. If enabled, pass nil so AniList returns both.
	enableAdult := false
	if currentSettings, settingsErr := h.getSettings(c); settingsErr == nil && currentSettings.GetAnilist() != nil {
		enableAdult = currentSettings.GetAnilist().EnableAdultContent
	}
	var isAdultPtr *bool
	if !enableAdult {
		falseVal := false
		isAdultPtr = &falseVal
	}

	// Checked before the cache lookup too, so a forced test always exercises a fresh SIMKL call
	// rather than serving a previously-cached real AniList result.
	if simklClient, ok := h.forcedSimklClient(c); ok {
		if fallback, fbErr := simklCalendarFallback(c.Request().Context(), simklClient); fbErr == nil {
			return h.RespondWithData(c, fallback)
		}
	}

	sortParts := make([]string, len(p.Sort))
	for i, s := range p.Sort {
		sortParts[i] = string(*s)
	}
	cacheKey := fmt.Sprintf("%s-%d-%s-%v", *p.Season, *p.SeasonYear, strings.Join(sortParts, ","), isAdultPtr != nil)
	if cached, ok := anilistListSeasonAnimeCache.Get(cacheKey); ok {
		return h.RespondWithData(c, cached)
	}

	perPage := 50
	results := make([]*anilist.BaseAnime, 0, 100)
	page := 1
	cacheLayer := shared_platform.NewCacheLayer(h.App.AnilistClientRef)
	for {
		ret, err := anilist.ListAnimeM(
			cacheLayer,
			&page,
			nil, // search
			&perPage,
			p.Sort,
			nil, // status
			nil, // genres
			nil, // tags
			nil, // averageScoreGreater
			p.Season,
			p.SeasonYear,
			nil, // format
			isAdultPtr,
			nil, // countryOfOrigin
			h.App.Logger,
			h.App.GetUserAnilistToken(),
		)
		if err != nil {
			if simklClient, ok := h.simklClientForProfile(c); ok && shouldTrySimklDiscoveryFallback(simklClient.ClientID()) {
				if fallback, fbErr := simklCalendarFallback(c.Request().Context(), simklClient); fbErr == nil {
					return h.RespondWithData(c, fallback)
				}
			}
			return h.RespondWithError(c, err)
		}
		if ret == nil || ret.GetPage() == nil {
			break
		}

		media := ret.GetPage().GetMedia()
		if len(media) == 0 {
			break
		}
		results = append(results, media...)

		pageInfo := ret.GetPage().GetPageInfo()
		if pageInfo == nil || pageInfo.GetHasNextPage() == nil || !*pageInfo.GetHasNextPage() {
			break
		}

		page++
		// Safety cap: 20 * 50 = 1000 entries. No real AniList season exceeds this.
		if page > 20 {
			break
		}
	}

	anilistListSeasonAnimeCache.SetT(cacheKey, results, time.Minute*10)

	return h.RespondWithData(c, results)
}

// HandleAnilistListRecentAiringAnime
//
//	@summary returns a list of recently aired anime.
//	@desc This is used by the "Schedule" page to display recently aired anime.
//	@route /api/v1/anilist/list-recent-anime [POST]
//	@returns anilist.ListRecentAnime
func (h *Handler) HandleAnilistListRecentAiringAnime(c echo.Context) error {

	type body struct {
		Page            *int                  `json:"page,omitempty"`
		Search          *string               `json:"search,omitempty"`
		PerPage         *int                  `json:"perPage,omitempty"`
		AiringAtGreater *int                  `json:"airingAt_greater,omitempty"`
		AiringAtLesser  *int                  `json:"airingAt_lesser,omitempty"`
		NotYetAired     *bool                 `json:"notYetAired,omitempty"`
		Sort            []*anilist.AiringSort `json:"sort,omitempty"`
	}

	p := new(body)
	if err := c.Bind(p); err != nil {
		return h.RespondWithError(c, err)
	}

	if p.Page == nil || p.PerPage == nil {
		*p.Page = 1
		*p.PerPage = 50
	}

	// Checked before the cache lookup too, so a forced test always exercises a fresh SIMKL call
	// rather than serving a previously-cached real AniList result.
	if simklClient, ok := h.forcedSimklClient(c); ok {
		if schedules, fbErr := simklCalendarFallbackSchedules(c.Request().Context(), simklClient, p.AiringAtGreater, p.AiringAtLesser); fbErr == nil {
			return h.RespondWithData(c, &anilist.ListRecentAnime{
				Page: &anilist.ListRecentAnime_Page{
					AiringSchedules: schedules,
				},
			})
		}
	}

	cacheKey := fmt.Sprintf("%v-%v-%v-%v-%v-%v-%v", p.Page, p.Search, p.PerPage, p.AiringAtGreater, p.AiringAtLesser, p.NotYetAired, p.Sort)

	cached, ok := anilistListRecentAnimeCache.Get(cacheKey)
	if ok {
		return h.RespondWithData(c, cached)
	}

	ret, err := anilist.ListRecentAiringAnimeM(
		h.App.AnilistPlatformRef.Get().GetAnilistClient(),
		p.Page,
		p.Search,
		p.PerPage,
		p.AiringAtGreater,
		p.AiringAtLesser,
		p.NotYetAired,
		p.Sort,
		h.App.Logger,
		h.App.GetUserAnilistToken(),
	)
	if err != nil {
		if simklClient, ok := h.simklClientForProfile(c); ok && shouldTrySimklDiscoveryFallback(simklClient.ClientID()) {
			if schedules, fbErr := simklCalendarFallbackSchedules(c.Request().Context(), simklClient, p.AiringAtGreater, p.AiringAtLesser); fbErr == nil {
				return h.RespondWithData(c, &anilist.ListRecentAnime{
					Page: &anilist.ListRecentAnime_Page{
						AiringSchedules: schedules,
					},
				})
			}
		}
		return h.RespondWithError(c, err)
	}

	anilistListRecentAnimeCache.SetT(cacheKey, ret, time.Hour*1)

	return h.RespondWithData(c, ret)
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

var anilistMissedSequelsCache = result.NewCache[int, []*anilist.BaseAnime]()

// HandleAnilistListMissedSequels
//
//	@summary returns a list of sequels not in the user's list.
//	@desc This is used by the "Discover" page to display sequels the user may have missed.
//	@route /api/v1/anilist/list-missed-sequels [GET]
//	@returns []anilist.BaseAnime
func (h *Handler) HandleAnilistListMissedSequels(c echo.Context) error {

	cached, ok := anilistMissedSequelsCache.Get(1)
	if ok {
		return h.RespondWithData(c, cached)
	}

	// Get complete anime collection
	animeCollection, err := h.getAnilistPlatform(c).GetAnimeCollectionWithRelations(c.Request().Context())
	if err != nil {
		return h.RespondWithError(c, err)
	}

	ret, err := anilist.ListMissedSequels(
		h.App.AnilistPlatformRef.Get().GetAnilistClient(),
		animeCollection,
		h.App.Logger,
		h.App.GetUserAnilistToken(),
	)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	anilistMissedSequelsCache.SetT(1, ret, time.Hour*4)

	return h.RespondWithData(c, ret)
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

var anilistStatsCache = result.NewCache[int, *anilist.Stats]()

// HandleGetAniListStats
//
//	@summary returns the anilist stats.
//	@desc This returns the AniList stats for the user.
//	@route /api/v1/anilist/stats [GET]
//	@returns anilist.Stats
func (h *Handler) HandleGetAniListStats(c echo.Context) error {
	cached, ok := anilistStatsCache.Get(0)
	if ok {
		return h.RespondWithData(c, cached)
	}

	stats, err := h.getAnilistPlatform(c).GetViewerStats(c.Request().Context())
	if err != nil {
		return h.RespondWithError(c, err)
	}

	ret, err := anilist.GetStats(
		c.Request().Context(),
		stats,
	)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	anilistStatsCache.SetT(0, ret, time.Hour*1)

	return h.RespondWithData(c, ret)
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// HandleGetAnilistCacheLayerStatus
//
//	@summary returns the status of the AniList cache layer.
//	@desc This returns the status of the AniList cache layer.
//	@route /api/v1/anilist/cache-layer/status [GET]
//	@returns bool
func (h *Handler) HandleGetAnilistCacheLayerStatus(c echo.Context) error {
	return h.RespondWithData(c, shared_platform.IsWorking.Load())
}

// HandleToggleAnilistCacheLayerStatus
//
//	@summary toggles the status of the AniList cache layer.
//	@desc This toggles the status of the AniList cache layer.
//	@route /api/v1/anilist/cache-layer/status [POST]
//	@returns bool
func (h *Handler) HandleToggleAnilistCacheLayerStatus(c echo.Context) error {
	shared_platform.IsWorking.Store(!shared_platform.IsWorking.Load())
	return h.RespondWithData(c, shared_platform.IsWorking.Load())
}
