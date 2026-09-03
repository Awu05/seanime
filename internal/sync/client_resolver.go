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

// resolvingClient re-resolves the access token on every call instead of caching it, so a
// disconnect/reconnect takes effect immediately without recreating MirroringPlatform.
type resolvingClient struct {
	httpClient *http.Client
	profileID  string
	lookup     TokenLookup
}

func NewResolvingSimklClient(httpClient *http.Client, profileID string, lookup TokenLookup) simkl.Client {
	return &resolvingClient{httpClient: httpClient, profileID: profileID, lookup: lookup}
}

func (r *resolvingClient) client() (simkl.Client, bool) {
	token, ok := r.lookup(r.profileID)
	if !ok {
		return nil, false
	}
	return simkl.NewAPIClient(r.httpClient, token), true
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
