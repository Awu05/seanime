package core

import (
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
)

// envAdminCredentials returns the Docker admin credentials bootstrapAdminFromEnv creates the first
// admin from.
func envAdminCredentials(getenv func(string) string) (username, password string, ok bool) {
	username, password = getenv("SEANIME_ADMIN_USERNAME"), getenv("SEANIME_ADMIN_PASSWORD")
	return username, password, username != "" && password != ""
}

// profilesEnabledAtBoot reports whether this boot ends with profiles on, counting an admin that
// module init will create from environment variables.
func profilesEnabledAtBoot(adminExists, isDesktopSidecar bool, getenv func(string) string) bool {
	if adminExists {
		return true
	}
	_, _, ok := envAdminCredentials(getenv)
	return ok && !isDesktopSidecar
}

// ignoreOfflineModeWithProfiles turns off an offline mode saved before profiles were set up. It
// syncs only the main account, so every other profile would start with an empty library.
func ignoreOfflineModeWithProfiles(cfg *Config, profilesEnabled bool, logger *zerolog.Logger) {
	if !cfg.Server.Offline || !profilesEnabled {
		return
	}
	logger.Warn().Msg("app: Offline mode isn't available when profiles are enabled, starting online")
	cfg.Server.Offline = false
	viper.Set("server.offline", false)
	if err := viper.WriteConfig(); err != nil {
		logger.Err(err).Msg("app: Failed to write config after turning off offline mode")
	}
}
