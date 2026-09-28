package core

import (
	"seanime/internal/util"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// A config saved before profiles were set up must not start the server in offline mode.
func TestIgnoreOfflineModeWithProfiles(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Set("server.offline", true)
	cfg := &Config{}
	cfg.Server.Offline = true

	ignoreOfflineModeWithProfiles(cfg, true, util.NewLogger())

	require.False(t, cfg.Server.Offline)
	require.False(t, viper.GetBool("server.offline"))
}

func TestKeepOfflineModeWithoutProfiles(t *testing.T) {
	t.Cleanup(viper.Reset)
	cfg := &Config{}
	cfg.Server.Offline = true

	ignoreOfflineModeWithProfiles(cfg, false, util.NewLogger())

	require.True(t, cfg.Server.Offline)
}
