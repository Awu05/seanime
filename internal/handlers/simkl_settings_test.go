package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShouldEnableSimklSync(t *testing.T) {
	t.Run("connected and toggled on", func(t *testing.T) {
		assert.True(t, shouldEnableSimklSync(true, true))
	})
	t.Run("toggled on but not connected", func(t *testing.T) {
		assert.False(t, shouldEnableSimklSync(false, true))
	})
	t.Run("connected but toggled off", func(t *testing.T) {
		assert.False(t, shouldEnableSimklSync(true, false))
	})
}
