package handlers

import (
	"testing"

	"seanime/internal/platforms/shared_platform"

	"github.com/stretchr/testify/assert"
)

func TestStatus_AnilistHealthyReflectsIsWorking(t *testing.T) {
	shared_platform.IsWorking.Store(false)
	status := &Status{AnilistHealthy: shared_platform.IsWorking.Load()}
	assert.False(t, status.AnilistHealthy)

	shared_platform.IsWorking.Store(true)
	status = &Status{AnilistHealthy: shared_platform.IsWorking.Load()}
	assert.True(t, status.AnilistHealthy)
}
