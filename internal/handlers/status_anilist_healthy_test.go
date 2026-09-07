package handlers

import (
	"testing"

	"seanime/internal/core"
	"seanime/internal/platforms/shared_platform"

	"github.com/stretchr/testify/assert"
)

// TestStatus_AnilistHealthyReflectsIsWorking verifies that newRestrictedStatus() (a real constructor)
// wires up the AnilistHealthy field based on shared_platform.IsWorking. This closes the gap where
// a regression (e.g., deleting the AnilistHealthy: ... line from the constructor) would compile fine
// but fail at runtime.
func TestStatus_AnilistHealthyReflectsIsWorking(t *testing.T) {
	cfg := &core.Config{}
	// Initialize the Server struct field to avoid nil pointer dereference
	cfg.Server.Password = ""
	h := &Handler{App: &core.App{Config: cfg}}

	t.Run("reflects false state", func(t *testing.T) {
		shared_platform.IsWorking.Store(false)
		status := h.newRestrictedStatus()
		assert.False(t, status.AnilistHealthy)
	})

	t.Run("reflects true state", func(t *testing.T) {
		shared_platform.IsWorking.Store(true)
		status := h.newRestrictedStatus()
		assert.True(t, status.AnilistHealthy)
	})

	// Restore the package-level default for other tests
	shared_platform.IsWorking.Store(true)
}
