package core

import (
	"seanime/internal/database/models"
	"seanime/internal/user"

	"github.com/goccy/go-json"
)

type ProfileContext struct {
	ProfileID string
	Account   *models.Account
	User      *user.User
	Token     string
}

func (a *App) GetProfileContext(profileID string) *ProfileContext {
	if profileID == "" {
		return &ProfileContext{
			User:  a.GetUser(),
			Token: a.GetUserAnilistToken(),
		}
	}

	acc, err := a.Database.GetAccountByProfileID(profileID)
	if err != nil || acc.Token == "" {
		return &ProfileContext{
			ProfileID: profileID,
			User:      user.NewSimulatedUser(),
		}
	}

	u, err := user.NewUser(acc)
	if err != nil {
		return &ProfileContext{
			ProfileID: profileID,
			Account:   acc,
			User:      user.NewSimulatedUser(),
			Token:     acc.Token,
		}
	}

	return &ProfileContext{
		ProfileID: profileID,
		Account:   acc,
		User:      u,
		Token:     acc.Token,
	}
}

type OverridableSettings struct {
	TorrentProvider          *string `json:"torrentProvider,omitempty"`
	HideAudienceScore       *bool   `json:"hideAudienceScore,omitempty"`
	EnableAdultContent       *bool   `json:"enableAdultContent,omitempty"`
	BlurAdultContent         *bool   `json:"blurAdultContent,omitempty"`
	AutoUpdateProgress       *bool   `json:"autoUpdateProgress,omitempty"`
	AutoPlayNextEpisode      *bool   `json:"autoPlayNextEpisode,omitempty"`
	EnableWatchContinuity    *bool   `json:"enableWatchContinuity,omitempty"`
	DisableAnimeCardTrailers *bool   `json:"disableAnimeCardTrailers,omitempty"`
	EnableOnlinestream       *bool   `json:"enableOnlinestream,omitempty"`
	EnableManga              *bool   `json:"enableManga,omitempty"`
}

func (a *App) GetMergedSettingsForProfile(profileID string) *models.Settings {
	settings := a.Settings
	if settings == nil || profileID == "" {
		return settings
	}

	ps, err := a.Database.GetProfileSettings(profileID)
	if err != nil || ps.Overrides == "" {
		return settings
	}

	var overrides OverridableSettings
	if err := json.Unmarshal([]byte(ps.Overrides), &overrides); err != nil {
		a.Logger.Error().Err(err).Msg("app: Failed to parse profile settings overrides")
		return settings
	}

	copied := *settings
	if copied.Library != nil {
		lib := *copied.Library
		copied.Library = &lib
	}
	if copied.Anilist != nil {
		ani := *copied.Anilist
		copied.Anilist = &ani
	}

	if overrides.TorrentProvider != nil && copied.Library != nil {
		copied.Library.TorrentProvider = *overrides.TorrentProvider
	}
	if overrides.AutoUpdateProgress != nil && copied.Library != nil {
		copied.Library.AutoUpdateProgress = *overrides.AutoUpdateProgress
	}
	if overrides.AutoPlayNextEpisode != nil && copied.Library != nil {
		copied.Library.AutoPlayNextEpisode = *overrides.AutoPlayNextEpisode
	}
	if overrides.EnableWatchContinuity != nil && copied.Library != nil {
		copied.Library.EnableWatchContinuity = *overrides.EnableWatchContinuity
	}
	if overrides.DisableAnimeCardTrailers != nil && copied.Library != nil {
		copied.Library.DisableAnimeCardTrailers = *overrides.DisableAnimeCardTrailers
	}
	if overrides.EnableOnlinestream != nil && copied.Library != nil {
		copied.Library.EnableOnlinestream = *overrides.EnableOnlinestream
	}
	if overrides.EnableManga != nil && copied.Library != nil {
		copied.Library.EnableManga = *overrides.EnableManga
	}
	if overrides.HideAudienceScore != nil && copied.Anilist != nil {
		copied.Anilist.HideAudienceScore = *overrides.HideAudienceScore
	}
	if overrides.EnableAdultContent != nil && copied.Anilist != nil {
		copied.Anilist.EnableAdultContent = *overrides.EnableAdultContent
	}
	if overrides.BlurAdultContent != nil && copied.Anilist != nil {
		copied.Anilist.BlurAdultContent = *overrides.BlurAdultContent
	}

	return &copied
}
