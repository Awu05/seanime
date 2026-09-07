package sync

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestResolvingDiscoverySimklClient_ReResolvesClientIDOnEveryCall guards against the exact bug
// this type was introduced to fix: a discoverySimklClient built once with a snapshotted client_id
// (via a plain simkl.NewAPIClient(..., clientID)) would keep using a stale value indefinitely,
// since the wrapped platform holding it is cached per-profile and never rebuilt on a settings
// save alone (see internal/core/simkl_wiring.go's wrapAnilistPlatform). resolvingDiscoveryClient
// must call clientIDFor fresh on every method call instead, mirroring resolvingClient's existing
// guarantee for Component 1's client. The actual HTTP calls are left to fail (cancelled context,
// no real network call) - this test only cares whether the lookup itself was re-invoked.
func TestResolvingDiscoverySimklClient_ReResolvesClientIDOnEveryCall(t *testing.T) {
	var seen []string
	clientIDFor := func(profileID string) string {
		id := "client-" + profileID
		seen = append(seen, id)
		return id
	}

	client := NewResolvingDiscoverySimklClient(http.DefaultClient, "profile-1", clientIDFor)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // fail fast instead of making a real network call

	_, _, _ = client.SearchIDByAnilist(ctx, 101922)
	_, _ = client.GetAnimeDetails(ctx, 46994)

	assert.Equal(t, []string{"client-profile-1", "client-profile-1"}, seen,
		"clientIDFor must be called fresh for every method call, not cached from construction time")
}
