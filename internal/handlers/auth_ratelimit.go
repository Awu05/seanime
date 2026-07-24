package handlers

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// authRateLimiter is a small in-memory per-IP failure limiter for credential
// endpoints. bcrypt alone does not stop online brute force of short PINs and
// access codes on a network-exposed server.
type authRateLimiter struct {
	mu       sync.Mutex
	failures map[string]*authFailureState
}

type authFailureState struct {
	count       int
	blockedTill time.Time
	lastFailure time.Time
}

const (
	authFailureThreshold = 5
	authFailureWindow    = 10 * time.Minute
	authBaseBlock        = 30 * time.Second
	authMaxBlock         = 10 * time.Minute
)

var authLimiter = &authRateLimiter{failures: make(map[string]*authFailureState)}

// check returns false (with a retry-after hint) while the client is blocked.
func (rl *authRateLimiter) check(ip string) (time.Duration, bool) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	st, exists := rl.failures[ip]
	if !exists {
		return 0, true
	}
	if time.Since(st.lastFailure) > authFailureWindow {
		delete(rl.failures, ip)
		return 0, true
	}
	if until := time.Until(st.blockedTill); until > 0 {
		return until, false
	}
	return 0, true
}

// fail records a failed credential attempt; after the threshold, blocks with
// exponential backoff.
func (rl *authRateLimiter) fail(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	st, exists := rl.failures[ip]
	if !exists || time.Since(st.lastFailure) > authFailureWindow {
		st = &authFailureState{}
		rl.failures[ip] = st
	}
	st.count++
	st.lastFailure = time.Now()
	if st.count >= authFailureThreshold {
		block := authBaseBlock << uint(st.count-authFailureThreshold)
		if block > authMaxBlock || block <= 0 {
			block = authMaxBlock
		}
		st.blockedTill = time.Now().Add(block)
	}
	// Opportunistic cleanup so the map can't grow unboundedly
	if len(rl.failures) > 1000 {
		for k, v := range rl.failures {
			if time.Since(v.lastFailure) > authFailureWindow {
				delete(rl.failures, k)
			}
		}
	}
}

func (rl *authRateLimiter) success(ip string) {
	rl.mu.Lock()
	delete(rl.failures, ip)
	rl.mu.Unlock()
}

// checkAuthRateLimit responds with 429 when the client is currently blocked.
// Returns nil when the attempt may proceed.
func (h *Handler) checkAuthRateLimit(c echo.Context) error {
	retryAfter, ok := authLimiter.check(c.RealIP())
	if ok {
		return nil
	}
	c.Response().Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
	return c.JSON(http.StatusTooManyRequests, map[string]string{"error": "Too many attempts, try again later"})
}
