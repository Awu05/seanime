package sync

import (
	"context"
	"errors"
	"net/http"
	"seanime/internal/api/simkl"
)

// TokenLookup returns the current SIMKL access token for a profile, or ("", false) if not
// connected. Backed by db.Database.GetSimklAccount in production.
type TokenLookup func(profileID string) (accessToken string, ok bool)

// ClientIdLookup returns the profile's own SIMKL app client ID, or "" if none is configured yet.
// Backed by db.Database.GetSimklSettings in production.
type ClientIdLookup func(profileID string) (clientID string)

// resolvingClient re-resolves the access token and client ID on every call instead of caching
// them, so a disconnect/reconnect or a client ID change takes effect immediately without
// recreating MirroringPlatform.
type resolvingClient struct {
	httpClient  *http.Client
	profileID   string
	lookup      TokenLookup
	clientIDFor ClientIdLookup
}

func NewResolvingSimklClient(httpClient *http.Client, profileID string, lookup TokenLookup, clientIDFor ClientIdLookup) simkl.Client {
	return &resolvingClient{httpClient: httpClient, profileID: profileID, lookup: lookup, clientIDFor: clientIDFor}
}

func (r *resolvingClient) client() (simkl.Client, bool) {
	token, ok := r.lookup(r.profileID)
	if !ok {
		return nil, false
	}
	return simkl.NewAPIClient(r.httpClient, token, r.clientIDFor(r.profileID)), true
}

// resolvingDiscoveryClient mirrors resolvingClient's "re-resolve on every call" guarantee for
// Component 4's public-endpoint fallback (GetAnimeDetails). It needs no access token - only the
// client_id - but must see a client_id change take effect immediately too, same as every other
// SIMKL client built in wrapAnilistPlatform; a client_id captured once at construction time and
// baked into a plain *simkl.APIClient would keep using a stale (or empty) value indefinitely,
// since the wrapped platform that owns it is cached per-profile and never rebuilt on a settings
// save alone.
type resolvingDiscoveryClient struct {
	httpClient  *http.Client
	profileID   string
	clientIDFor ClientIdLookup
}

// NewResolvingDiscoverySimklClient returns a discoverySimklClient backed by a fresh
// simkl.APIClient (client_id only, no access token) on every call.
func NewResolvingDiscoverySimklClient(httpClient *http.Client, profileID string, clientIDFor ClientIdLookup) discoverySimklClient {
	return &resolvingDiscoveryClient{httpClient: httpClient, profileID: profileID, clientIDFor: clientIDFor}
}

func (r *resolvingDiscoveryClient) client() *simkl.APIClient {
	return simkl.NewAPIClient(r.httpClient, "", r.clientIDFor(r.profileID))
}

func (r *resolvingDiscoveryClient) SearchIDByAnilist(ctx context.Context, anilistID int) (int, bool, error) {
	return r.client().SearchIDByAnilist(ctx, anilistID)
}

func (r *resolvingDiscoveryClient) GetAnimeDetails(ctx context.Context, simklID int) (*simkl.AnimeDetail, error) {
	return r.client().GetAnimeDetails(ctx, simklID)
}

func (r *resolvingClient) AddToList(ctx context.Context, anilistID int, status string) error {
	c, ok := r.client()
	if !ok {
		return errors.New("simkl: not connected")
	}
	return c.AddToList(ctx, anilistID, status)
}

func (r *resolvingClient) MarkProgress(ctx context.Context, anilistID int, episode int) error {
	c, ok := r.client()
	if !ok {
		return errors.New("simkl: not connected")
	}
	return c.MarkProgress(ctx, anilistID, episode)
}

func (r *resolvingClient) RemoveEntry(ctx context.Context, anilistID int) error {
	c, ok := r.client()
	if !ok {
		return errors.New("simkl: not connected")
	}
	return c.RemoveEntry(ctx, anilistID)
}

func (r *resolvingClient) SetRating(ctx context.Context, anilistID int, rating int) error {
	c, ok := r.client()
	if !ok {
		return errors.New("simkl: not connected")
	}
	return c.SetRating(ctx, anilistID, rating)
}

func (r *resolvingClient) RemoveRating(ctx context.Context, anilistID int) error {
	c, ok := r.client()
	if !ok {
		return errors.New("simkl: not connected")
	}
	return c.RemoveRating(ctx, anilistID)
}

func (r *resolvingClient) TestConnection(ctx context.Context) error {
	c, ok := r.client()
	if !ok {
		return errors.New("simkl: not connected")
	}
	return c.TestConnection(ctx)
}

func (r *resolvingClient) GetAllItems(ctx context.Context) ([]simkl.AllItemsEntry, error) {
	c, ok := r.client()
	if !ok {
		return nil, errors.New("simkl: not connected")
	}
	return c.GetAllItems(ctx)
}

func (r *resolvingClient) AddToListBatch(ctx context.Context, items []simkl.AddToListItem) error {
	c, ok := r.client()
	if !ok {
		return errors.New("simkl: not connected")
	}
	return c.AddToListBatch(ctx, items)
}

func (r *resolvingClient) MarkProgressBatch(ctx context.Context, items []simkl.ProgressItem) error {
	c, ok := r.client()
	if !ok {
		return errors.New("simkl: not connected")
	}
	return c.MarkProgressBatch(ctx, items)
}

func (r *resolvingClient) RemoveEntryBatch(ctx context.Context, anilistIDs []int) error {
	c, ok := r.client()
	if !ok {
		return errors.New("simkl: not connected")
	}
	return c.RemoveEntryBatch(ctx, anilistIDs)
}

func (r *resolvingClient) SetRatingBatch(ctx context.Context, items []simkl.RatingItem) error {
	c, ok := r.client()
	if !ok {
		return errors.New("simkl: not connected")
	}
	return c.SetRatingBatch(ctx, items)
}

func (r *resolvingClient) RemoveRatingBatch(ctx context.Context, anilistIDs []int) error {
	c, ok := r.client()
	if !ok {
		return errors.New("simkl: not connected")
	}
	return c.RemoveRatingBatch(ctx, anilistIDs)
}
