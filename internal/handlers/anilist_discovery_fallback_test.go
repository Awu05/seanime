package handlers

import (
	"testing"

	"seanime/internal/platforms/shared_platform"

	"github.com/stretchr/testify/assert"
)

// This is a narrow unit test of the gating logic extracted into shouldTrySimklDiscoveryFallback,
// not a full end-to-end handler test (this codebase's existing handler tests, if any, don't spin
// up a full echo server with a real SIMKL mock per the research pass - keep this test at the same
// level of the existing test suite rather than introducing a new heavier pattern).
func TestShouldTrySimklDiscoveryFallback(t *testing.T) {
	shared_platform.IsWorking.Store(true)
	assert.False(t, shouldTrySimklDiscoveryFallback("some-client-id"), "must not fall back while AniList is healthy")

	shared_platform.IsWorking.Store(false)
	assert.False(t, shouldTrySimklDiscoveryFallback(""), "must not fall back with no client id configured")
	assert.True(t, shouldTrySimklDiscoveryFallback("some-client-id"))

	shared_platform.IsWorking.Store(true) // restore package-level default for other tests
}
