package anilist

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestPacer(clock *testClock) *aniListPacer {
	p := newAniListPacer()
	p.now = clock.Now
	return p
}

// recordSleep advances the fake clock by each requested delay and records it.
func recordSleep(clock *testClock, delays *[]time.Duration) requestSleepFunc {
	return func(_ context.Context, delay time.Duration) error {
		*delays = append(*delays, delay)
		clock.Advance(delay)
		return nil
	}
}

func rateHeaders(limit, remaining string) http.Header {
	h := http.Header{}
	h.Set("X-RateLimit-Limit", limit)
	h.Set("X-RateLimit-Remaining", remaining)
	return h
}

func TestPacerServesBurstThenSpreadsRequests(t *testing.T) {
	clock := &testClock{now: time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC)}
	p := newTestPacer(clock)
	var delays []time.Duration

	// The default limit of 30/min allows a burst of 5.
	for range 5 {
		require.NoError(t, p.Wait(context.Background(), recordSleep(clock, &delays)))
	}
	require.Empty(t, delays)

	require.NoError(t, p.Wait(context.Background(), recordSleep(clock, &delays)))
	require.Equal(t, []time.Duration{2 * time.Second}, delays)
}

func TestPacerFollowsLimitHeader(t *testing.T) {
	clock := &testClock{now: time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC)}
	p := newTestPacer(clock)
	p.Observe(rateHeaders("90", "89"))
	var delays []time.Duration

	for range 15 {
		require.NoError(t, p.Wait(context.Background(), recordSleep(clock, &delays)))
	}
	require.Empty(t, delays, "a limit of 90/min allows a burst of 15")

	require.NoError(t, p.Wait(context.Background(), recordSleep(clock, &delays)))
	require.Len(t, delays, 1)
	require.InDelta(t, float64(time.Second*60/90), float64(delays[0]), float64(time.Millisecond))
}

func TestPacerLowersTokensToRemaining(t *testing.T) {
	clock := &testClock{now: time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC)}
	p := newTestPacer(clock)
	p.Observe(rateHeaders("30", "3"))
	var delays []time.Duration

	require.NoError(t, p.Wait(context.Background(), recordSleep(clock, &delays)))
	require.Empty(t, delays)
	require.NoError(t, p.Wait(context.Background(), recordSleep(clock, &delays)))
	require.Equal(t, []time.Duration{2 * time.Second}, delays)
}

// With nothing left in AniList's window, the pacer stays the margin below it instead of sending at
// the full rate until AniList refuses.
func TestPacerDelaysWhenAniListReportsNoneRemaining(t *testing.T) {
	clock := &testClock{now: time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC)}
	p := newTestPacer(clock)
	p.Observe(rateHeaders("30", "0"))
	var delays []time.Duration

	require.NoError(t, p.Wait(context.Background(), recordSleep(clock, &delays)))
	require.Equal(t, []time.Duration{6 * time.Second}, delays)
}

func TestPacerBackgroundKeepsHalfTheBurstForBrowsing(t *testing.T) {
	clock := &testClock{now: time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC)}
	p := newTestPacer(clock)
	var delays []time.Duration

	for range 3 {
		require.NoError(t, p.Wait(context.Background(), recordSleep(clock, &delays)))
	}
	// 2 of 5 tokens left; background needs 1 plus half the burst (3.5), so it waits 1.5 tokens.
	require.NoError(t, p.Wait(WithBackgroundPriority(context.Background()), recordSleep(clock, &delays)))
	require.Equal(t, []time.Duration{3 * time.Second}, delays)
}

func TestPacerBackgroundYieldsToWaitingBrowsing(t *testing.T) {
	clock := &testClock{now: time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC)}
	p := newTestPacer(clock)
	p.browsingWaiting = 1
	var delays []time.Duration

	err := p.Wait(WithBackgroundPriority(context.Background()), func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		clock.Advance(delay)
		p.browsingWaiting = 0 // the browsing request was served
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, []time.Duration{2 * time.Second}, delays)
}

func TestPacerCancelledWaitReturnsContextError(t *testing.T) {
	clock := &testClock{now: time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC)}
	p := newTestPacer(clock)
	p.Observe(rateHeaders("30", "0"))

	err := p.Wait(context.Background(), func(context.Context, time.Duration) error { return context.Canceled })
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, -2.0, p.tokens, "a cancelled wait must not take a token")
}
