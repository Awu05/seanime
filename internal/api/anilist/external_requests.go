package anilist

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// IsAPIURL reports whether rawURL points at the AniList API in use, whose rate limit requests made
// outside this package share.
func IsAPIURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	api, err := url.Parse(currentRequestProvider().ApiUrl())
	return err == nil && strings.EqualFold(u.Host, api.Host)
}

// PaceExternalRequest waits for the rate limit on the background lane, so plugin requests only use
// what browsing can spare.
func PaceExternalRequest(ctx context.Context) error {
	return sharedAniListPacer.Wait(WithBackgroundPriority(ctx), nil)
}

// ObserveExternalResponse follows the rate limit a response to an outside request reports, and stops
// every AniList request until the reset time when it was rate limited.
func ObserveExternalResponse(resp *http.Response) {
	observeResponse(sharedAniListPacer, resp, func(waitSeconds int) {
		notifyAniListRateLimit(sharedAniListPacer.logger, waitSeconds)
	})
}
