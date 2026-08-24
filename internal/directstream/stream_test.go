package directstream

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSwapServeContentCancelFuncCancelsPreviousSequential guards the basic invariant:
// swapping in a new cancel func cancels the previous one exactly once, and never the new one.
func TestSwapServeContentCancelFuncCancelsPreviousSequential(t *testing.T) {
	s := &BaseStream{}

	var calls int32
	makeCancel := func() func() {
		return func() { atomic.AddInt32(&calls, 1) }
	}

	s.swapServeContentCancelFunc(makeCancel())
	require.EqualValues(t, 0, atomic.LoadInt32(&calls), "the first cancel func should not be called yet")

	s.swapServeContentCancelFunc(makeCancel())
	require.EqualValues(t, 1, atomic.LoadInt32(&calls), "swapping in a new cancel func should cancel the previous one")

	s.swapServeContentCancelFunc(makeCancel())
	require.EqualValues(t, 2, atomic.LoadInt32(&calls))
}

// TestSwapServeContentCancelFuncConcurrent guards against the actual bug: overlapping Range
// requests for the same stream (normal during seeking/scrubbing) call this concurrently from
// multiple goroutines. Without synchronization, a store can be lost - the previous cancel func
// gets overwritten without ever being invoked, leaking the request it belonged to (or, worse,
// its context never gets cancelled and it keeps writing to an abandoned response). If every
// swap is correctly serialized, exactly N-1 of the N installed cancel funcs get called by the
// swaps themselves, and the Nth (whichever ends up as the final survivor) is left for the
// caller to cancel - so the total after that must be exactly N, never less.
func TestSwapServeContentCancelFuncConcurrent(t *testing.T) {
	s := &BaseStream{}

	const n = 50
	var calls int32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			s.swapServeContentCancelFunc(func() { atomic.AddInt32(&calls, 1) })
		}()
	}
	wg.Wait()

	// Cancel whichever cancel func survived as the final one.
	s.serveContentCancelFuncMu.Lock()
	survivor := s.serveContentCancelFunc
	s.serveContentCancelFuncMu.Unlock()
	require.NotNil(t, survivor, "expected a survivor cancel func to remain installed")
	survivor()

	require.EqualValues(t, n, atomic.LoadInt32(&calls), "expected every installed cancel func to be called exactly once with no lost updates")
}
