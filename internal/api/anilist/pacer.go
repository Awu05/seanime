package anilist

import (
	"context"
	"math"
	"net/http"
	"seanime/internal/util"
	"slices"
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

// IsBackgroundPriority reports whether ctx was marked by WithBackgroundPriority.
func IsBackgroundPriority(ctx context.Context) bool {
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
	mu           sync.Mutex
	now          func() time.Time
	logger       *zerolog.Logger
	limit        float64 // requests per minute; 0 leaves an endpoint that reported none unpaced
	tokens       float64
	updatedAt    time.Time
	blockedUntil time.Time
	// Browsing requests wait per profile, and profiles take turns so one can't hold up the rest.
	waiting map[string]int
	turns   []string
}

func newAniListPacer() *aniListPacer {
	return &aniListPacer{now: time.Now, logger: util.NewLogger(), limit: defaultAniListLimit, waiting: make(map[string]int)}
}

// Wait blocks until the request may be sent, or until ctx is done.
func (p *aniListPacer) Wait(ctx context.Context, sleep requestSleepFunc) error {
	if sleep == nil {
		sleep = sleepWithContext
	}
	background := IsBackgroundPriority(ctx)
	profileID := util.ProfileIDFromContext(ctx)
	if !background {
		p.mu.Lock()
		p.joinLocked(profileID)
		p.mu.Unlock()
	}

	var waited time.Duration
	for {
		p.mu.Lock()
		delay := p.reserveLocked(background, profileID)
		if delay == 0 && !background {
			p.leaveLocked(profileID, true)
		}
		p.mu.Unlock()
		if delay == 0 {
			break
		}
		if err := sleep(ctx, delay); err != nil {
			if !background {
				p.mu.Lock()
				p.leaveLocked(profileID, false)
				p.mu.Unlock()
			}
			return err
		}
		waited += delay
	}
	if waited > time.Second {
		p.logger.Debug().Bool("background", background).Str("waited", waited.String()).Msg("anilist: Request paced")
	}
	return nil
}

// joinLocked queues a browsing request. A profile with nothing waiting takes its turn after every
// profile already waiting.
func (p *aniListPacer) joinLocked(profileID string) {
	if p.waiting[profileID] == 0 {
		p.turns = append(p.turns, profileID)
	}
	p.waiting[profileID]++
}

// leaveLocked removes a browsing request. A profile that was served goes to the back of the turns.
func (p *aniListPacer) leaveLocked(profileID string, served bool) {
	p.waiting[profileID]--
	done := p.waiting[profileID] == 0
	if done {
		delete(p.waiting, profileID)
	}
	if done || served {
		p.turns = slices.DeleteFunc(p.turns, func(id string) bool { return id == profileID })
	}
	if served && !done {
		p.turns = append(p.turns, profileID)
	}
}

// reserveLocked takes a token and returns 0, or returns how long to wait before trying again.
func (p *aniListPacer) reserveLocked(background bool, profileID string) time.Duration {
	now := p.now()
	if now.Before(p.blockedUntil) {
		return p.blockedUntil.Sub(now)
	}
	if p.limit == 0 {
		return 0
	}
	p.refillLocked(now)

	need := 1.0
	if background {
		if len(p.turns) > 0 {
			return p.tokenDuration(1)
		}
		need += p.capacity() / 2
	} else if p.turns[0] != profileID {
		// Another profile's turn: look again once it could have taken a token.
		return p.tokenDuration(1)
	}
	if p.tokens >= need {
		p.tokens--
		return 0
	}
	return p.tokenDuration(need - p.tokens)
}

// capacity is the burst size. At least 2 keeps both lanes reachable at any limit, since background
// needs one token plus half the burst.
func (p *aniListPacer) capacity() float64 {
	return max(p.limit/6, 2)
}

// tokenDuration is how long the bucket takes to refill n tokens, rounded up so a fractional shortfall
// never becomes a zero wait that skips taking a token.
func (p *aniListPacer) tokenDuration(n float64) time.Duration {
	return time.Duration(math.Ceil(n * 60 / p.limit * float64(time.Second)))
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

// Reset starts afresh for a new endpoint. AniList's own API is paced before its first response;
// another endpoint may have no limit, so it waits until it reports one.
func (p *aniListPacer) Reset(official bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.limit = 0
	if official {
		p.limit = defaultAniListLimit
	}
	p.tokens = 0
	p.updatedAt = time.Time{}
	p.blockedUntil = time.Time{}
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
