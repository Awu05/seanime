package util

import (
	"sync"
	"testing"
)

// Every logger shares the buffer that is later written to the log file, so logging from several
// goroutines at once must be safe. Run with -race to catch a regression.
func TestLoggersWriteToSharedBufferConcurrently(t *testing.T) {
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			logger := NewLogger()
			for j := range 50 {
				logger.Debug().Int("goroutine", i).Int("line", j).Msg("concurrent log line")
			}
		})
	}
	wg.Wait()
}
