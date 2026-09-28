package anilist

import (
	"context"
	"net/http"
	"seanime/internal/util"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type backgroundPriorityKey struct{}

// WithBackgroundPriority marks AniList requests made with ctx as background work, which only uses
// the request budget that people browsing can spare.
func WithBackgroundPriority(ctx context.Context) context.Context {
	return context.WithValue(ctx, backgroundPriorityKey{}, true)
}

func isBackgroundPriority(ctx context.Context) bool {
	background, _ := ctx.Value(backgroundPriorityKey{}).(bool)
	return background
}

type requestPacer interface {
	Wait(ctx context.Context, sleep requestSleepFunc) error
	BlockUntil(until time.Time) bool
	Observe(headers http.Header)
}

type requestSleepFunc func(ctx context.Context, delay time.Duration) error

const (
	// defaultAniListLimit is AniList's degraded limit, used until a response reports the current one.
	defaultAniListLimit = 30
	// remainingMargin keeps the pacer slightly below AniList's own count of remaining requests.
	remainingMargin = 2
)

// aniListPacer is a token bucket shared by every AniList request, since AniList limits by IP.
type aniListPacer struct {
	mu              sync.Mutex
	now             func() time.Time
	logger          *zerolog.Logger
	limit           float64 // requests per minute
	tokens          float64
	updatedAt       time.Time
	blockedUntil    time.Time
	browsingWaiting int
}

func newAniListPacer() *aniListPacer {
	return &aniListPacer{now: time.Now, logger: util.NewLogger(), limit: defaultAniListLimit}
}

// Wait blocks until the request may be sent, or until ctx is done.
func (p *aniListPacer) Wait(ctx context.Context, sleep requestSleepFunc) error {
	if sleep == nil {
		sleep = sleepWithContext
	}
	background := isBackgroundPriority(ctx)
	if !background {
		p.mu.Lock()
		p.browsingWaiting++
		p.mu.Unlock()
		defer func() {
			p.mu.Lock()
			p.browsingWaiting--
			p.mu.Unlock()
		}()
	}

	var waited time.Duration
	for {
		p.mu.Lock()
		delay := p.reserveLocked(background)
		p.mu.Unlock()
		if delay == 0 {
			break
		}
		if err := sleep(ctx, delay); err != nil {
			return err
		}
		waited += delay
	}
	if waited > time.Second {
		p.logger.Debug().Bool("background", background).Str("waited", waited.String()).Msg("anilist: Request paced")
	}
	return nil
}

// reserveLocked takes a token and returns 0, or returns how long to wait before trying again.
func (p *aniListPacer) reserveLocked(background bool) time.Duration {
	now := p.now()
	if now.Before(p.blockedUntil) {
		return p.blockedUntil.Sub(now)
	}
	p.refillLocked(now)

	need := 1.0
	if background {
		if p.browsingWaiting > 0 {
			return p.tokenDuration(1)
		}
		need += p.capacity() / 2
	}
	if p.tokens >= need {
		p.tokens--
		return 0
	}
	return p.tokenDuration(need - p.tokens)
}

func (p *aniListPacer) capacity() float64 {
	return p.limit / 6
}

// tokenDuration is how long the bucket takes to refill n tokens.
func (p *aniListPacer) tokenDuration(n float64) time.Duration {
	return time.Duration(n * 60 / p.limit * float64(time.Second))
}

func (p *aniListPacer) refillLocked(now time.Time) {
	if p.updatedAt.IsZero() {
		p.tokens = p.capacity()
	} else {
		p.tokens = min(p.capacity(), p.tokens+now.Sub(p.updatedAt).Seconds()*p.limit/60)
	}
	p.updatedAt = now
}

// Observe follows the limit AniList reports and never runs ahead of its remaining count, which
// also covers requests the pacer does not see (plugins, a restart mid-minute).
func (p *aniListPacer) Observe(headers http.Header) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if limit, err := strconv.Atoi(headers.Get("X-RateLimit-Limit")); err == nil && limit > 0 {
		p.limit = float64(limit)
	}
	p.refillLocked(p.now())
	if remaining, err := strconv.Atoi(headers.Get("X-RateLimit-Remaining")); err == nil {
		p.tokens = min(p.tokens, float64(remaining-remainingMargin))
	}
}

// BlockUntil stops all requests until the time AniList gave in a rate-limited response.
func (p *aniListPacer) BlockUntil(until time.Time) bool {
	if until.IsZero() {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !until.After(p.now()) || !until.After(p.blockedUntil) {
		return false
	}
	p.blockedUntil = until
	return true
}
