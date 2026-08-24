package directstream

import (
	"net/http/httptest"
	"seanime/internal/util"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDebridStreamWriteAndFlushToCacheReturnsErrorWhenCacheClosed guards against a nil-pointer
// panic: every other access to httpStream (initializeStream, getReader, Close) is guarded by
// cacheMu and checks for nil, but the stream-serving handler used to call
// s.httpStream.WriteAndFlush(...) directly. If Close() (triggered by Terminate on a rapid
// episode switch) runs concurrently and nils out httpStream, the in-flight request panics
// instead of failing gracefully.
func TestDebridStreamWriteAndFlushToCacheReturnsErrorWhenCacheClosed(t *testing.T) {
	s := newTestDebridStream() // httpStream is nil, as it would be after Close()

	err := s.writeAndFlushToCache(strings.NewReader("hello"), httptest.NewRecorder(), 0)
	require.Error(t, err, "expected an error rather than a nil-pointer panic when httpStream is nil")
}

// TestDebridStreamWriteAndFlushToCacheConcurrentWithCloseNeverPanics stresses the actual race:
// a request thread calling writeAndFlushToCache while Close() runs on another goroutine.
func TestDebridStreamWriteAndFlushToCacheConcurrentWithCloseNeverPanics(t *testing.T) {
	for i := 0; i < 200; i++ {
		s := newTestDebridStream()

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = s.Close()
		}()
		go func() {
			defer wg.Done()
			require.NotPanics(t, func() {
				_ = s.writeAndFlushToCache(strings.NewReader("hello"), httptest.NewRecorder(), 0)
			})
		}()
		wg.Wait()
	}
}

// TestNakamaWriteAndFlushToCacheReturnsErrorWhenCacheClosed mirrors the DebridStream fix for
// Nakama's identical httpStream/cacheMu pattern.
func TestNakamaWriteAndFlushToCacheReturnsErrorWhenCacheClosed(t *testing.T) {
	s := &Nakama{BaseStream: BaseStream{logger: util.NewLogger()}}

	err := s.writeAndFlushToCache(strings.NewReader("hello"), httptest.NewRecorder(), 0)
	require.Error(t, err, "expected an error rather than a nil-pointer panic when httpStream is nil")
}

func newTestDebridStream() *DebridStream {
	return &DebridStream{BaseStream: BaseStream{logger: util.NewLogger()}}
}
