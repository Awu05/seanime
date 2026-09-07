package handlers

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"seanime/internal/api/anilist"
	"seanime/internal/core"
	"seanime/internal/database/db"
	"seanime/internal/database/models"
	"seanime/internal/platforms/shared_platform"
	"seanime/internal/torrent_clients/qbittorrent"
	"seanime/internal/torrents/torrent"
	"seanime/internal/util"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/samber/lo"
)

// HandleGetSettings
//
//	@summary returns the app settings.
//	@route /api/v1/settings [GET]
//	@returns models.Settings
func (h *Handler) HandleGetSettings(c echo.Context) error {
	settings, err := h.getSettings(c)

	if err != nil {
		return h.RespondWithError(c, err)
	}
	if settings.ID == 0 {
		return h.RespondWithError(c, errors.New(runtime.GOOS))
	}

	clientSettings := db.CloneSettings(settings)
	db.VirtualizeSettingsPaths(clientSettings)

	return h.RespondWithData(c, clientSettings)
}

// HandleGettingStarted
//
//	@summary updates the app settings.
//	@desc This will update the app settings.
//	@desc The client should re-fetch the server status after this.
//	@route /api/v1/start [POST]
//	@returns handlers.Status
func (h *Handler) HandleGettingStarted(c echo.Context) error {

	profileID := core.GetProfileIDFromContext(c)

	type body struct {
		Library                models.LibrarySettings      `json:"library"`
		MediaPlayer            models.MediaPlayerSettings  `json:"mediaPlayer"`
		Torrent                models.TorrentSettings      `json:"torrent"`
		Anilist                models.AnilistSettings      `json:"anilist"`
		Discord                models.DiscordSettings      `json:"discord"`
		Manga                  models.MangaSettings        `json:"manga"`
		Notifications          models.NotificationSettings `json:"notifications"`
		Nakama                 models.NakamaSettings       `json:"nakama"`
		EnableTranscode        bool                        `json:"enableTranscode"`
		EnableTorrentStreaming bool                        `json:"enableTorrentStreaming"`
		DebridProvider         string                      `json:"debridProvider"`
		DebridApiKey           string                      `json:"debridApiKey"`
		DebridApiUrl           string                      `json:"debridApiUrl"`
	}
	var b body

	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	// Resolve incoming virtual paths to physical paths on iOS
	if b.Library.LibraryPath != "" {
		b.Library.LibraryPath = util.ResolvePhysicalPath(b.Library.LibraryPath)
	}
	for i, p := range b.Library.LibraryPaths {
		b.Library.LibraryPaths[i] = util.ResolvePhysicalPath(p)
	}
	if b.Manga.LocalSourceDirectory != "" {
		b.Manga.LocalSourceDirectory = util.ResolvePhysicalPath(b.Manga.LocalSourceDirectory)
	}

	prevSettings, _ := h.getSettings(c)
	if err := h.guardStrictSettingsMutation(c, prevSettings, &b.Library, &b.Manga); err != nil {
		return err
	}
	if err := h.guardPrivilegedSettingsMutation(c, prevSettings, &b.MediaPlayer, &b.Torrent); err != nil {
		return err
	}

	// Check settings
	if b.Library.LibraryPaths == nil {
		b.Library.LibraryPaths = []string{}
	}
	b.Library.LibraryPath = filepath.ToSlash(b.Library.LibraryPath)

	b.Library.IncludeOnlineStreamingInLibrary = b.Library.EnableOnlinestream

	newSettings := &models.Settings{
		Library:       &b.Library,
		MediaPlayer:   &b.MediaPlayer,
		Torrent:       &b.Torrent,
		Anilist:       &b.Anilist,
		Discord:       &b.Discord,
		Manga:         &b.Manga,
		Notifications: &b.Notifications,
		Nakama:        &b.Nakama,
		AutoDownloader: &models.AutoDownloaderSettings{
			Provider:              b.Library.TorrentProvider,
			Interval:              20,
			Enabled:               false,
			DownloadAutomatically: true,
			EnableEnhancedQueries: true,
		},
	}

	var settings *models.Settings
	var err error
	if h.App.MultiUserEnabled && profileID != "" {
		settings, err = h.App.Database.UpsertSettingsForProfile(profileID, newSettings)
	} else {
		newSettings.BaseModel = models.BaseModel{
			ID:        1,
			UpdatedAt: time.Now(),
		}
		settings, err = h.App.Database.UpsertSettings(newSettings)
	}

	if err != nil {
		return h.RespondWithError(c, err)
	}

	// The branches below write GLOBAL secondary settings (torrentstream,
	// mediastream, debrid provider/API key) — only admins may change those.
	// A non-admin re-running getting-started only writes their own settings.
	isAdmin := core.GetIsAdminFromContext(c)

	if b.EnableTorrentStreaming && isAdmin {
		go func() {
			defer util.HandlePanicThen(func() {})
			prev, found := h.App.Database.GetTorrentstreamSettings()
			if found {
				prev.Enabled = true
				prev.IncludeInLibrary = true
				_, _ = h.App.Database.UpsertTorrentstreamSettings(prev)
			}
		}()
	}

	if b.EnableTranscode && isAdmin {
		go func() {
			defer util.HandlePanicThen(func() {})
			prev, found := h.App.Database.GetMediastreamSettings()
			if found {
				prev.TranscodeEnabled = true
				_, _ = h.App.Database.UpsertMediastreamSettings(prev)
			}
		}()
	}

	if b.DebridProvider != "" && b.DebridProvider != "none" && isAdmin {
		go func() {
			defer util.HandlePanicThen(func() {})
			prev, found := h.App.Database.GetDebridSettings()
			if found {
				prev.Enabled = true
				prev.Provider = b.DebridProvider
				prev.ApiKey = b.DebridApiKey
				prev.ApiUrl = b.DebridApiUrl
				prev.IncludeDebridStreamInLibrary = true
				_, _ = h.App.Database.UpsertDebridSettings(prev)
			}
		}()
	}

	h.App.WSEventManager.SendToProfile(profileID, "settings", settings)

	status := h.NewStatus(c)

	// Refresh modules that depend on the settings
	h.App.InitOrRefreshModules(profileID)

	return h.RespondWithData(c, status)
}

// HandleSaveSettings
//
//	@summary updates the app settings.
//	@desc This will update the app settings.
//	@desc The client should re-fetch the server status after this.
//	@route /api/v1/settings [PATCH]
//	@returns handlers.Status
func (h *Handler) HandleSaveSettings(c echo.Context) error {

	profileID := core.GetProfileIDFromContext(c)

	type body struct {
		Library       models.LibrarySettings      `json:"library"`
		MediaPlayer   models.MediaPlayerSettings  `json:"mediaPlayer"`
		Torrent       models.TorrentSettings      `json:"torrent"`
		Anilist       models.AnilistSettings      `json:"anilist"`
		Discord       models.DiscordSettings      `json:"discord"`
		Manga         models.MangaSettings        `json:"manga"`
		Notifications models.NotificationSettings `json:"notifications"`
		Nakama        models.NakamaSettings       `json:"nakama"`
	}
	var b body

	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	// Resolve incoming virtual paths to physical paths on iOS
	if util.IsIOS() {
		if b.Library.LibraryPath != "" {
			b.Library.LibraryPath = util.ResolvePhysicalPath(b.Library.LibraryPath)
		}
		for i, p := range b.Library.LibraryPaths {
			b.Library.LibraryPaths[i] = util.ResolvePhysicalPath(p)
		}
		if b.Manga.LocalSourceDirectory != "" {
			b.Manga.LocalSourceDirectory = util.ResolvePhysicalPath(b.Manga.LocalSourceDirectory)
		}
	}

	prevSettings, err := h.getSettings(c)
	if err := h.guardStrictSettingsMutation(c, prevSettings, &b.Library, &b.Manga); err != nil {
		return err
	}
	if err := h.guardPrivilegedSettingsMutation(c, prevSettings, &b.MediaPlayer, &b.Torrent); err != nil {
		return err
	}

	b.MediaPlayer.VlcPath = strings.TrimSpace(strings.Trim(b.MediaPlayer.VlcPath, "\""))
	b.MediaPlayer.MpcPath = strings.TrimSpace(strings.Trim(b.MediaPlayer.MpcPath, "\""))
	b.MediaPlayer.MpvPath = strings.TrimSpace(strings.Trim(b.MediaPlayer.MpvPath, "\""))
	b.MediaPlayer.IinaPath = strings.TrimSpace(strings.Trim(b.MediaPlayer.IinaPath, "\""))

	b.Torrent.QBittorrentPath = strings.TrimSpace(strings.Trim(b.Torrent.QBittorrentPath, "\""))
	b.Torrent.TransmissionPath = strings.TrimSpace(strings.Trim(b.Torrent.TransmissionPath, "\""))

	// Block the save if qBittorrent is the active torrent client but can't actually be reached -
	// previously a bad host/port/credential typo (or qBittorrent simply being down) would save
	// silently, with the only feedback being a background log line nobody sees. This runs on
	// every save (not just when the fields changed) so a save while qBittorrent happens to be
	// down is never silently accepted either.
	if shouldTestQbittorrentConnection(&b.Torrent) {
		loginFn := qbittorrentLoginFor(&b.Torrent, h.App.Logger)
		if err := testQbittorrentConnection(loginFn); err != nil {
			return h.RespondWithError(c, fmt.Errorf("could not connect to qBittorrent: %w", err))
		}
	}

	if b.Library.LibraryPath != "" {
		b.Library.LibraryPath = filepath.ToSlash(filepath.Clean(b.Library.LibraryPath))
	}

	if b.Library.LibraryPaths == nil || b.Library.LibraryPath == "" {
		b.Library.LibraryPaths = []string{}
	}

	for i, path := range b.Library.LibraryPaths {
		b.Library.LibraryPaths[i] = filepath.ToSlash(filepath.Clean(path))
	}

	b.Library.LibraryPaths = lo.Filter(b.Library.LibraryPaths, func(s string, _ int) bool {
		if s == "" || util.IsSameDir(s, b.Library.LibraryPath) {
			return false
		}
		info, err := os.Stat(util.ResolvePhysicalPath(s))
		if err != nil {
			return false
		}
		return info.IsDir()
	})

	// Check that any library paths are not subdirectories of each other
	for i, path1 := range b.Library.LibraryPaths {
		if util.IsSubdirectory(b.Library.LibraryPath, path1) || util.IsSubdirectory(path1, b.Library.LibraryPath) {
			return h.RespondWithError(c, errors.New("library paths cannot be subdirectories of each other"))
		}
		for j, path2 := range b.Library.LibraryPaths {
			if i != j && util.IsSubdirectory(path1, path2) {
				return h.RespondWithError(c, errors.New("library paths cannot be subdirectories of each other"))
			}
		}
	}

	autoDownloaderSettings := models.AutoDownloaderSettings{}
	if prevSettings != nil && prevSettings.AutoDownloader != nil {
		autoDownloaderSettings = *prevSettings.AutoDownloader
	}
	// Disable auto-downloader if the torrent provider is set to none
	if b.Library.TorrentProvider == torrent.ProviderNone && autoDownloaderSettings.Enabled {
		h.App.Logger.Debug().Msg("app: Disabling auto-downloader because the torrent provider is set to none")
		autoDownloaderSettings.Enabled = false
	}

	newSettings := &models.Settings{
		Library:        &b.Library,
		MediaPlayer:    &b.MediaPlayer,
		Torrent:        &b.Torrent,
		Anilist:        &b.Anilist,
		Manga:          &b.Manga,
		Discord:        &b.Discord,
		Notifications:  &b.Notifications,
		Nakama:         &b.Nakama,
		AutoDownloader: &autoDownloaderSettings,
	}

	var settings *models.Settings
	if h.App.MultiUserEnabled && profileID != "" {
		settings, err = h.App.Database.UpsertSettingsForProfile(profileID, newSettings)
	} else {
		newSettings.BaseModel = models.BaseModel{
			ID:        1,
			UpdatedAt: time.Now(),
		}
		settings, err = h.App.Database.UpsertSettings(newSettings)
	}

	if err != nil {
		return h.RespondWithError(c, err)
	}

	h.App.WSEventManager.SendToProfile(profileID, "settings", settings)

	// Sync adult content setting to AniList if it changed
	if prevSettings != nil && prevSettings.Anilist != nil && prevSettings.Anilist.EnableAdultContent != b.Anilist.EnableAdultContent {
		go func() {
			defer util.HandlePanicThen(func() {})
			var account *models.Account
			var accErr error
			if h.App.MultiUserEnabled && profileID != "" {
				account, accErr = h.App.Database.GetAccountByProfileID(profileID)
			} else {
				account, accErr = h.App.Database.GetAccount()
			}
			if accErr != nil || account == nil || account.Token == "" {
				return
			}
			_, _ = anilist.CustomQuery(map[string]interface{}{
				"query":     "mutation UpdateUser($displayAdultContent: Boolean) { UpdateUser(displayAdultContent: $displayAdultContent) { id } }",
				"variables": map[string]interface{}{"displayAdultContent": b.Anilist.EnableAdultContent},
			}, h.App.Logger, account.Token)
		}()
	}

	// Refresh modules that depend on the settings
	h.App.InitOrRefreshModules(profileID)

	// InitOrRefreshModules always reads the GLOBAL settings row (see its own implementation),
	// which does not reflect what was actually just saved when this save went to a per-profile row
	// instead (multi-user mode, UpsertSettingsForProfile above) - without this, toggling "Force
	// SIMKL fallback" on a multi-user instance would persist correctly but never actually take
	// effect. Re-applying the just-submitted value directly here also means the status response
	// below reflects it immediately, rather than waiting for the frontend's next status poll.
	shared_platform.ForceSimklFallback.Store(b.Anilist.ForceSimklFallback)

	status := h.NewStatus(c)

	return h.RespondWithData(c, status)
}

// HandlePatchSetting
//
//	@summary patches a specific app setting.
//	@desc This updates a single setting path and refreshes the server status.
//	@route /api/v1/settings/path [PATCH]
//	@returns handlers.Status
func (h *Handler) HandlePatchSetting(c echo.Context) error {
	type body struct {
		Path  string      `json:"path"`
		Value interface{} `json:"value"`
	}

	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	b.Path = strings.TrimSpace(b.Path)
	if b.Path == "" {
		return h.RespondWithError(c, errors.New("settings path is empty"))
	}

	prevSettings, err := h.getSettings(c)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	nextSettings, err := models.SetSettingsPath(prevSettings, b.Path, b.Value)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	if err := h.guardStrictSettingsMutation(c, prevSettings, nextSettings.Library, nextSettings.Manga); err != nil {
		return err
	}
	if err := h.guardPrivilegedSettingsMutation(c, prevSettings, nextSettings.MediaPlayer, nextSettings.Torrent); err != nil {
		return err
	}

	profileID := core.GetProfileIDFromContext(c)

	var settings *models.Settings
	if h.App.MultiUserEnabled && profileID != "" {
		settings, err = h.App.Database.UpsertSettingsForProfile(profileID, nextSettings)
	} else {
		nextSettings.BaseModel = models.BaseModel{
			ID:        1,
			UpdatedAt: time.Now(),
		}
		settings, err = h.App.Database.UpsertSettings(nextSettings)
	}
	if err != nil {
		return h.RespondWithError(c, err)
	}

	h.App.WSEventManager.SendToProfile(profileID, "settings", settings)

	h.App.InitOrRefreshModules(profileID)

	// See the matching comment in HandleSaveSettings: InitOrRefreshModules reads the global
	// settings row, which a per-profile patch (multi-user mode) never touches.
	if nextSettings.Anilist != nil {
		shared_platform.ForceSimklFallback.Store(nextSettings.Anilist.ForceSimklFallback)
	}

	status := h.NewStatus(c)

	return h.RespondWithData(c, status)
}

// HandleSaveAutoDownloaderSettings
//
//	@summary updates the auto-downloader settings.
//	@route /api/v1/settings/auto-downloader [PATCH]
//	@returns bool
func (h *Handler) HandleSaveAutoDownloaderSettings(c echo.Context) error {

	type body struct {
		Provider              string `json:"provider"`
		Interval              int    `json:"interval"`
		Enabled               bool   `json:"enabled"`
		DownloadAutomatically bool   `json:"downloadAutomatically"`
		EnableEnhancedQueries bool   `json:"enableEnhancedQueries"`
		EnableSeasonCheck     bool   `json:"enableSeasonCheck"`
		UseDebrid             bool   `json:"useDebrid"`
	}

	var b body

	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	currSettings, err := h.getSettings(c)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	// Validation
	if b.Interval < 15 {
		return h.RespondWithError(c, errors.New("interval must be at least 15 minutes"))
	}

	autoDownloaderSettings := &models.AutoDownloaderSettings{
		Provider:              b.Provider,
		Interval:              b.Interval,
		Enabled:               b.Enabled,
		DownloadAutomatically: b.DownloadAutomatically,
		EnableEnhancedQueries: b.EnableEnhancedQueries,
		EnableSeasonCheck:     b.EnableSeasonCheck,
		UseDebrid:             b.UseDebrid,
	}

	profileID := core.GetProfileIDFromContext(c)

	// Shallow-copy before mutating — currSettings may alias db.CurrSettings (the global cache)
	// or another caller's view of the same row; direct mutation would race concurrent readers.
	nextSettings := *currSettings
	nextSettings.AutoDownloader = autoDownloaderSettings

	if h.App.MultiUserEnabled && profileID != "" {
		_, err = h.App.Database.UpsertSettingsForProfile(profileID, &nextSettings)
	} else {
		nextSettings.BaseModel = models.BaseModel{
			ID:        1,
			UpdatedAt: time.Now(),
		}
		_, err = h.App.Database.UpsertSettings(&nextSettings)
	}
	if err != nil {
		return h.RespondWithError(c, err)
	}

	// Update Auto Downloader settings
	h.App.AutoDownloader.SetSettings(autoDownloaderSettings)

	return h.RespondWithData(c, true)
}

// qbittorrentTestConnectionBody carries the (not necessarily saved) qBittorrent connection
// fields to test, so the form's current values can be tested before they're persisted.
type qbittorrentTestConnectionBody struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Path     string `json:"path"`
}

func (b qbittorrentTestConnectionBody) toTorrentSettings() *models.TorrentSettings {
	return &models.TorrentSettings{
		QBittorrentHost:     b.Host,
		QBittorrentPort:     b.Port,
		QBittorrentUsername: b.Username,
		QBittorrentPassword: b.Password,
		QBittorrentPath:     b.Path,
	}
}

// HandleTestQbittorrentConnection
//
//	@summary tests connectivity to qBittorrent with the given (not necessarily saved) settings.
//	@desc Lets the client verify a qBittorrent host/port/credentials combination before saving it.
//	@route /api/v1/settings/qbittorrent/test-connection [POST]
//	@returns bool
func (h *Handler) HandleTestQbittorrentConnection(c echo.Context) error {
	var b qbittorrentTestConnectionBody
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	loginFn := qbittorrentLoginFor(b.toTorrentSettings(), h.App.Logger)
	if err := testQbittorrentConnection(loginFn); err != nil {
		return h.RespondWithError(c, fmt.Errorf("could not connect to qBittorrent: %w", err))
	}

	return h.RespondWithData(c, true)
}

// HandleSaveMediaPlayerSettings
//
//	@summary updates the media player settings.
//	@route /api/v1/settings/media-player [PATCH]
//	@returns bool
func (h *Handler) HandleSaveMediaPlayerSettings(c echo.Context) error {

	type body struct {
		MediaPlayer *models.MediaPlayerSettings `json:"mediaPlayer"`
	}

	var b body

	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	currSettings, err := h.getSettings(c)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	if err := h.guardPrivilegedSettingsMutation(c, currSettings, b.MediaPlayer, nil); err != nil {
		return err
	}

	profileID := core.GetProfileIDFromContext(c)

	// Shallow-copy before mutating — currSettings may alias db.CurrSettings (the global cache)
	// or another caller's view of the same row; direct mutation would race concurrent readers.
	nextSettings := *currSettings
	nextSettings.MediaPlayer = b.MediaPlayer

	if h.App.MultiUserEnabled && profileID != "" {
		_, err = h.App.Database.UpsertSettingsForProfile(profileID, &nextSettings)
	} else {
		nextSettings.BaseModel = models.BaseModel{
			ID:        1,
			UpdatedAt: time.Now(),
		}
		_, err = h.App.Database.UpsertSettings(&nextSettings)
	}
	if err != nil {
		return h.RespondWithError(c, err)
	}

	h.App.InitOrRefreshModules(profileID)

	return h.RespondWithData(c, true)
}

// qbittorrentConnectionTestTimeout bounds how long testQbittorrentConnection waits before failing
// closed. qbittorrent.Client's underlying http.Client has no timeout of its own, so an
// unreachable host could otherwise hang the settings save indefinitely. A var (not const) so
// tests can shrink it.
var qbittorrentConnectionTestTimeout = 8 * time.Second

// shouldTestQbittorrentConnection reports whether a settings save should verify qBittorrent
// connectivity before persisting: only relevant when qBittorrent is the active torrent client
// and a host is actually configured.
func shouldTestQbittorrentConnection(t *models.TorrentSettings) bool {
	return t.Default == "qbittorrent" && t.QBittorrentHost != ""
}

// qbittorrentLoginFor builds a login function for testQbittorrentConnection from the given
// (not-yet-persisted) torrent settings.
func qbittorrentLoginFor(t *models.TorrentSettings, logger *zerolog.Logger) func() error {
	client := qbittorrent.NewClient(&qbittorrent.NewClientOptions{
		Logger:   logger,
		Username: t.QBittorrentUsername,
		Password: t.QBittorrentPassword,
		Port:     t.QBittorrentPort,
		Host:     t.QBittorrentHost,
		Path:     t.QBittorrentPath,
	})
	return client.Login
}

// testQbittorrentConnection runs loginFn with a hard timeout (qbittorrentConnectionTestTimeout),
// since the underlying HTTP client has none of its own.
func testQbittorrentConnection(loginFn func() error) error {
	done := make(chan error, 1)
	go func() {
		done <- loginFn()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(qbittorrentConnectionTestTimeout):
		return errors.New("connection timed out")
	}
}
