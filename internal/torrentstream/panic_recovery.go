package torrentstream

import (
	"runtime/debug"

	"github.com/rs/zerolog"
)

// logRecoveredPanic logs a panic recovered from a background goroutine, including the panic
// value and a stack trace, so a crash here doesn't fail silently and become undiagnosable -
// it would otherwise just look like a stream that stopped responding, with nothing in the logs.
func logRecoveredPanic(logger *zerolog.Logger, source string, rec any) {
	logger.Error().
		Interface("recover", rec).
		Str("stack", string(debug.Stack())).
		Msgf("torrentstream: Recovered from panic in %s", source)
}
