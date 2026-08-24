package directstream

import (
	"seanime/internal/util"
	"seanime/internal/util/result"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestManagerShutdownClearsClientLoadMu guards against a regression where clientLoadMu (one
// *sync.Mutex per clientId ever seen, used to serialize loadStream per client) was never
// cleared, even on session eviction - a long-running server with many profile switches/
// reconnects would accumulate an entry per distinct clientId for the process lifetime.
func TestManagerShutdownClearsClientLoadMu(t *testing.T) {
	m := &Manager{
		Logger:  util.NewLogger(),
		streams: result.NewMap[string, Stream](),
	}

	unlock := m.lockClient("client-1")
	unlock()

	_, existed := m.clientLoadMu.Load("client-1")
	require.True(t, existed, "test setup: expected a per-client mutex to be registered after lockClient")

	m.Shutdown()

	_, stillExists := m.clientLoadMu.Load("client-1")
	require.False(t, stillExists, "expected Shutdown to clear per-client load mutexes")
}
