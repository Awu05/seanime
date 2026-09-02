package core

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryUntilSuccess(t *testing.T) {
	t.Run("succeeds on first attempt without sleeping", func(t *testing.T) {
		calls := 0
		sleeps := 0
		err := retryUntilSuccess(5, time.Second, func(time.Duration) { sleeps++ }, func() error {
			calls++
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, 1, calls)
		assert.Equal(t, 0, sleeps)
	})

	t.Run("retries after failures then succeeds", func(t *testing.T) {
		calls := 0
		sleeps := 0
		err := retryUntilSuccess(5, time.Second, func(time.Duration) { sleeps++ }, func() error {
			calls++
			if calls < 3 {
				return errors.New("still not ready")
			}
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, 3, calls)
		assert.Equal(t, 2, sleeps)
	})

	t.Run("returns the last error when every attempt fails", func(t *testing.T) {
		calls := 0
		wantErr := errors.New("permanently broken")
		err := retryUntilSuccess(4, time.Millisecond, func(time.Duration) {}, func() error {
			calls++
			return wantErr
		})
		require.ErrorIs(t, err, wantErr)
		assert.Equal(t, 4, calls)
	})
}
