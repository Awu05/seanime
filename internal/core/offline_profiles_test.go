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

// A Docker server's first admin is created from environment variables during module init, after
// the offline check runs, so it has to count as profiles already.
func TestProfilesEnabledAtBoot(t *testing.T) {
	envAdmin := func(key string) string {
		return map[string]string{"SEANIME_ADMIN_USERNAME": "admin", "SEANIME_ADMIN_PASSWORD": "pw"}[key]
	}
	noEnv := func(string) string { return "" }

	require.True(t, profilesEnabledAtBoot(true, false, noEnv))
	require.True(t, profilesEnabledAtBoot(false, false, envAdmin))
	require.False(t, profilesEnabledAtBoot(false, true, envAdmin), "desktop sidecar never bootstraps from env")
	require.False(t, profilesEnabledAtBoot(false, false, noEnv))
}
