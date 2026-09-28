package core

import (
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
)

// ignoreOfflineModeWithProfiles turns off an offline mode saved before profiles were set up. It
// syncs only the main account, so every other profile would start with an empty library.
func ignoreOfflineModeWithProfiles(cfg *Config, adminExists bool, logger *zerolog.Logger) {
	if !cfg.Server.Offline || !adminExists {
		return
	}
	logger.Warn().Msg("app: Offline mode isn't available when profiles are enabled, starting online")
	cfg.Server.Offline = false
	viper.Set("server.offline", false)
	if err := viper.WriteConfig(); err != nil {
		logger.Err(err).Msg("app: Failed to write config after turning off offline mode")
	}
}
