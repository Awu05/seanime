package testutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetConfig(t *testing.T) {
	cfg := InitTestProvider(t)
	assert.NotEqual(t, Config{}, *cfg)
}

func TestLoadConfig_IsolatedInstances(t *testing.T) {
	if _, err := readConfig(); err != nil {
		t.Skipf("test config unavailable: %v", err)
	}
	first := LoadConfig(t)
	second := LoadConfig(t)

	assert.NotSame(t, first, second)
	assert.Equal(t, *first, *second)

	first.Path.DataDir = t.TempDir()
	assert.NotEqual(t, first.Path.DataDir, second.Path.DataDir)
}

func TestInitTestProvider_DefaultsWithoutConfig(t *testing.T) {
	t.Setenv("TEST_CONFIG_PATH", t.TempDir())

	cfg := InitTestProvider(t)

	assert.NotNil(t, cfg)
	assert.Equal(t, defaultTestDatabaseName, cfg.Database.Name)
	assert.Empty(t, cfg.Path.DataDir)
	assert.False(t, cfg.Flags.EnableAnilistTests)
}

// A fresh checkout has no recorded fixtures, so tests that need them skip instead of failing.
func TestRequireAnilistFixturesSkipsMissingFixture(t *testing.T) {
	t.Setenv(RecordAnilistFixturesEnvName, "")
	var skipped bool
	t.Run("missing", func(t *testing.T) {
		defer func() { skipped = t.Skipped() }()
		RequireAnilistFixtures(t, "does-not-exist")
	})
	assert.True(t, skipped)
}

// While recording, the fixtures are about to be written, so the test must run.
func TestRequireAnilistFixturesRunsWhileRecording(t *testing.T) {
	t.Setenv(RecordAnilistFixturesEnvName, "true")
	var skipped bool
	t.Run("recording", func(t *testing.T) {
		defer func() { skipped = t.Skipped() }()
		RequireAnilistFixtures(t, "does-not-exist")
	})
	assert.False(t, skipped)
}
