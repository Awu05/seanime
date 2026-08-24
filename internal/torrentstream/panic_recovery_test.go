package torrentstream

import (
	"bytes"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// TestLogRecoveredPanicIncludesValueAndStack guards against a regression where a panic
// recovered from a background goroutine (native player termination, StopStream, media player
// event handling) was silently swallowed with no logging - making a real crash in one of those
// paths look like a stream that just stopped responding, with no trace in the logs.
func TestLogRecoveredPanicIncludesValueAndStack(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	logRecoveredPanic(&logger, "TestSource", "boom: nil pointer")

	out := buf.String()
	require.Contains(t, out, "TestSource", "expected the log line to name where the panic was recovered")
	require.Contains(t, out, "boom: nil pointer", "expected the log line to include the recovered panic value")
	require.Contains(t, out, "torrentstream/panic_recovery_test.go", "expected the log line to include a stack trace pointing back into this test")
}
