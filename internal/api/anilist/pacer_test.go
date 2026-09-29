package anilist

import (
	"context"
	"errors"
	"net/http"
	"seanime/internal/util"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// waitAll starts one Wait per profile ID in order, each after the one before it is queued, and
// returns the order they were served in.
func waitAll(t *testing.T, p *aniListPacer, profileIDs ...string) []string {
	t.Helper()
	var mu sync.Mutex
	var served []string
	var wg sync.WaitGroup
	for i, profileID := range profileIDs {
		wg.Go(func() {
			if err := p.Wait(util.ContextWithProfileID(context.Background(), profileID), nil); err != nil {
				t.Error(err)
			}
			mu.Lock()
			served = append(served, profileID)
			mu.Unlock()
		})
		require.Eventually(t, func() bool {
			p.mu.Lock()
			defer p.mu.Unlock()
			queued := 0
			for _, n := range p.waiting {
				queued += n
			}
			return queued == i+1
		}, time.Second, time.Millisecond)
	}
	wg.Wait()
	return served
}

// blockedPacer serves a token every 10ms after a one-second head start, so waiters queue before any
// is served.
func blockedPacer() *aniListPacer {
	p := newAniListPacer()
	p.Observe(rateHeaders("6000", "-98"))
	return p
}

func TestPacerTakesTurnsBetweenProfiles(t *testing.T) {
	for _, first := range []string{"a", ""} {
		served := waitAll(t, blockedPacer(), first, first, first, first, first, "b")
		require.Equal(t, "b", served[1], "profile %q's burst must not hold up b", first)
	}
}

func TestPacerServesOneProfileAtTheUsualRate(t *testing.T) {
	clock := &testClock{now: time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC)}
	p := newTestPacer(clock)
	ctx := util.ContextWithProfileID(context.Background(), "a")
	var delays []time.Duration

	for range 5 {
		require.NoError(t, p.Wait(ctx, recordSleep(clock, &delays)))
	}
	require.Empty(t, delays)
	require.NoError(t, p.Wait(ctx, recordSleep(clock, &delays)))
	require.Equal(t, []time.Duration{2 * time.Second}, delays)
}

func TestPacerDropsCancelledWaiterFromTurns(t *testing.T) {
	p := blockedPacer()
	ctx, cancel := context.WithCancel(util.ContextWithProfileID(context.Background(), "a"))
	cancelled := make(chan error, 1)
	go func() { cancelled <- p.Wait(ctx, nil) }()
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.waiting["a"] == 1
	}, time.Second, time.Millisecond)

	cancel()
	require.ErrorIs(t, <-cancelled, context.Canceled)
	require.NoError(t, p.Wait(util.ContextWithProfileID(context.Background(), "b"), nil))
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.turns)
	require.Empty(t, p.waiting)
}

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

// A limit that doesn't divide a minute evenly must still charge every request a token.
func TestPacerChargesEveryRequestAtUnevenRates(t *testing.T) {
	clock := &testClock{now: time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC)}
	p := newTestPacer(clock)
	p.Observe(rateHeaders("90", "89"))
	var delays []time.Duration

	for range 15 + 3 {
		require.NoError(t, p.Wait(context.Background(), recordSleep(clock, &delays)))
	}

	var waited time.Duration
	for _, d := range delays {
		waited += d
	}
	require.GreaterOrEqual(t, waited, 3*time.Second*60/90, "each request past the burst waits one token")
}

// Even a very low limit must let both lanes through eventually rather than wait forever.
func TestPacerServesBothLanesAtVeryLowLimits(t *testing.T) {
	clock := &testClock{now: time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC)}
	p := newTestPacer(clock)
	p.Observe(rateHeaders("5", "5"))
	sleeps := 0
	boundedSleep := func(_ context.Context, delay time.Duration) error {
		if sleeps++; sleeps > 20 {
			return errors.New("still waiting after 20 sleeps")
		}
		clock.Advance(delay)
		return nil
	}

	for range 3 {
		require.NoError(t, p.Wait(context.Background(), boundedSleep))
		require.NoError(t, p.Wait(WithBackgroundPriority(context.Background()), boundedSleep))
	}
}

// A custom endpoint may have no limit at all, so it isn't paced until it reports one.
func TestPacerDoesNotPaceEndpointUntilItReportsALimit(t *testing.T) {
	clock := &testClock{now: time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC)}
	p := newTestPacer(clock)
	p.Reset(false)
	var delays []time.Duration

	for range 20 {
		require.NoError(t, p.Wait(context.Background(), recordSleep(clock, &delays)))
	}
	require.Empty(t, delays)

	p.Observe(rateHeaders("30", "29"))
	for range 6 {
		require.NoError(t, p.Wait(context.Background(), recordSleep(clock, &delays)))
	}
	require.NotEmpty(t, delays, "a reported limit is paced")
}

func TestSwitchingEndpointResetsPacer(t *testing.T) {
	prevProvider := CurrentRequestProvider()
	t.Cleanup(func() { require.NoError(t, SetRequestProvider(prevProvider)) })

	require.NoError(t, UseCustomAPI(CustomClientConfig{Name: "mirror", Endpoint: "https://mirror.example.com/graphql"}))
	require.Zero(t, sharedAniListPacer.limit)

	UseOfficialAPI()
	require.EqualValues(t, defaultAniListLimit, sharedAniListPacer.limit)
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
	p.joinLocked("")
	var delays []time.Duration

	err := p.Wait(WithBackgroundPriority(context.Background()), func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		clock.Advance(delay)
		p.leaveLocked("", true) // the browsing request was served
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
