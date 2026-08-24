package httputil

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// TestFileStreamBroadcastsOnWrite guards against a regression where a reader blocked waiting
// for not-yet-downloaded data busy-polled a shared mutex every 10ms instead of being woken up
// when data actually arrives. Under a slow/stalled remote (exactly the condition streaming is
// most likely to hit), that produced continuous wake/lock churn contending with WriteAndFlush's
// own per-chunk lock, for as long as any reader was starved. This directly exercises the
// broadcast mechanism WriteAndFlush must use to wake waiting readers: the "no new data yet"
// channel must be closed (waking anyone blocked on it) and replaced with a fresh one after
// every write, with the close happening before the replacement is visible.
func TestFileStreamBroadcastsOnWrite(t *testing.T) {
	logger := zerolog.Nop()
	fs, err := NewFileStream(context.Background(), &logger, 10)
	require.NoError(t, err)
	t.Cleanup(func() { _ = fs.Close() })

	fs.mu.Lock()
	before := fs.dataAvailable
	fs.mu.Unlock()

	require.NoError(t, fs.WriteAndFlush(bytes.NewReader([]byte("hello")), &discardFlusher{}, 0))

	select {
	case <-before:
	default:
		t.Fatal("expected the pre-write dataAvailable channel to be closed by WriteAndFlush, waking any reader blocked on it")
	}

	fs.mu.Lock()
	after := fs.dataAvailable
	fs.mu.Unlock()

	require.NotEqual(t, before, after, "expected a fresh dataAvailable channel to be installed for the next wait")
	select {
	case <-after:
		t.Fatal("expected the new dataAvailable channel to still be open")
	default:
	}
}

// TestFileStreamReaderWakesUpOnNewData is an end-to-end sanity check that a reader blocked on a
// not-yet-available range actually completes once WriteAndFlush supplies the awaited bytes.
func TestFileStreamReaderWakesUpOnNewData(t *testing.T) {
	logger := zerolog.Nop()
	fs, err := NewFileStream(context.Background(), &logger, 10)
	require.NoError(t, err)
	t.Cleanup(func() { _ = fs.Close() })

	reader, err := fs.NewReader()
	require.NoError(t, err)

	readDone := make(chan struct {
		n   int
		err error
	}, 1)
	go func() {
		buf := make([]byte, 5)
		n, readErr := reader.Read(buf)
		readDone <- struct {
			n   int
			err error
		}{n, readErr}
	}()

	time.Sleep(20 * time.Millisecond) // let the reader goroutine actually start blocking

	writeDone := make(chan error, 1)
	go func() {
		writeDone <- fs.WriteAndFlush(bytes.NewReader([]byte("hello")), &discardFlusher{}, 0)
	}()

	select {
	case res := <-readDone:
		require.NoError(t, res.err)
		require.Equal(t, 5, res.n)
	case <-time.After(2 * time.Second):
		t.Fatal("reader never woke up after data became available")
	}

	require.NoError(t, <-writeDone)
}

// TestFileStreamReaderUnblocksOnContextCancel guards against losing responsiveness to context
// cancellation (e.g. stream termination) when switching the reader off a fixed poll interval.
func TestFileStreamReaderUnblocksOnContextCancel(t *testing.T) {
	logger := zerolog.Nop()
	ctx, cancel := context.WithCancel(context.Background())
	fs, err := NewFileStream(ctx, &logger, 10)
	require.NoError(t, err)
	t.Cleanup(func() { _ = fs.Close() })

	reader, err := fs.NewReader()
	require.NoError(t, err)

	readDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 5)
		_, readErr := reader.Read(buf)
		readDone <- readErr
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-readDone:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("reader did not unblock after context cancellation")
	}
}

type discardFlusher struct{}

func (d *discardFlusher) Write(p []byte) (int, error) { return len(p), nil }
