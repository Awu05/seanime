package simkl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"seanime/internal/constants"
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

// DefaultHTTPClient is the shared client every SIMKL call should use. http.DefaultClient has
// no timeout, so a hung SIMKL connection would block a mirrored list mutation (and therefore
// the HTTP request or playback progress update that triggered it) indefinitely.
var DefaultHTTPClient = &http.Client{Timeout: 10 * time.Second}

// Client is the interface MirroringPlatform depends on, so tests can substitute a fake
// instead of hitting the network.
type Client interface {
	AddToList(ctx context.Context, anilistID int, status string) error
	MarkProgress(ctx context.Context, anilistID int, episode int) error
	RemoveEntry(ctx context.Context, anilistID int) error
	SetRating(ctx context.Context, anilistID int, rating int) error
	RemoveRating(ctx context.Context, anilistID int) error
	TestConnection(ctx context.Context) error
	GetAllItems(ctx context.Context) ([]AllItemsEntry, error)

	// Batch variants used by the retry Worker to deliver many queued rows in as few SIMKL
	// requests as possible - see MaxBatchSize's doc comment. MirroringPlatform's live,
	// interactive mirroring never needs these; a user action always affects exactly one entry.
	AddToListBatch(ctx context.Context, items []AddToListItem) error
	MarkProgressBatch(ctx context.Context, items []ProgressItem) error
	RemoveEntryBatch(ctx context.Context, anilistIDs []int) error
	SetRatingBatch(ctx context.Context, items []RatingItem) error
	RemoveRatingBatch(ctx context.Context, anilistIDs []int) error
}

type APIClient struct {
	httpClient  *http.Client
	accessToken string
	clientID    string
	baseURL     string
}

func NewAPIClient(httpClient *http.Client, accessToken string, clientID string) *APIClient {
	return &APIClient{httpClient: httpClient, accessToken: accessToken, clientID: clientID, baseURL: defaultBaseURL}
}

// appName/appVersion are sent on every request per SIMKL's required query parameters - see
// https://simkl.docs.apiary.io. app-version tracks Seanime's own release version rather than a
// separate SIMKL-integration version, since there's no meaningful distinction to track here.
const appName = "seanime"

// postLimiter paces every POST sent to SIMKL, process-wide, to 1 every 1.1s (a hair under
// SIMKL's documented "1 POST request per second per client_id" cap - exceeding it is grounds
// for the client_id being suspended). Package-level rather than per-APIClient because a fresh
// APIClient is constructed on every call (see resolvingClient in package sync), so any
// per-instance state would never accumulate; a global limiter also correctly paces the two
// independent callers that both issue SIMKL POSTs - live mirrored writes (MirroringPlatform)
// and the queued-retry Worker - against each other, not just within themselves. GET requests
// (TestConnection, GetAllItems) aren't covered: SIMKL's docs only call out POST.
var postLimiter = rate.NewLimiter(rate.Every(1100*time.Millisecond), 1)

func (c *APIClient) do(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	if method == http.MethodPost {
		if err := postLimiter.Wait(ctx); err != nil {
			return nil, err
		}
	}

	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}

	reqURL, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, err
	}
	query := reqURL.Query()
	query.Set("client_id", c.clientID)
	query.Set("app-name", appName)
	query.Set("app-version", constants.Version)
	reqURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	// Every authenticated SIMKL endpoint requires this alongside the bearer token and the
	// client_id query parameter above - the access token alone identifies the user, but SIMKL
	// also needs to know which registered app is calling on their behalf.
	req.Header.Set("simkl-api-key", c.clientID)
	req.Header.Set("User-Agent", appName+"/"+constants.Version)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		defer res.Body.Close()
		return res, fmt.Errorf("simkl: request to %s failed with status %d", path, res.StatusCode)
	}
	return res, nil
}

// MaxBatchSize is the most items SIMKL's write endpoints accept in one call (per SIMKL's rate
// limit docs: "will happily process 50 items in one request"). Batch callers - currently just
// the retry Worker - must not exceed this; these methods don't chunk defensively since the one
// caller already respects it by construction, and silently chunking would hide partial-failure
// detail the caller needs to know which rows to retry.
const MaxBatchSize = 50

// AddToListItem, ProgressItem and RatingItem are the batch-call equivalents of
// AddToList/MarkProgress/SetRating's single-item parameters.
type AddToListItem struct {
	AnilistID int
	Status    string
}

type ProgressItem struct {
	AnilistID int
	Episode   int
}

type RatingItem struct {
	AnilistID int
	Rating    int
}

func (c *APIClient) AddToList(ctx context.Context, anilistID int, status string) error {
	return c.AddToListBatch(ctx, []AddToListItem{{AnilistID: anilistID, Status: status}})
}

func (c *APIClient) AddToListBatch(ctx context.Context, items []AddToListItem) error {
	shows := make([]ShowEntry, len(items))
	for i, item := range items {
		shows[i] = ShowEntry{Ids: Ids{Anilist: strconv.Itoa(item.AnilistID)}, To: item.Status}
	}
	res, err := c.do(ctx, http.MethodPost, "/sync/add-to-list", animeEnvelope{Anime: shows})
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return nil
}

func (c *APIClient) MarkProgress(ctx context.Context, anilistID int, episode int) error {
	return c.MarkProgressBatch(ctx, []ProgressItem{{AnilistID: anilistID, Episode: episode}})
}

func (c *APIClient) MarkProgressBatch(ctx context.Context, items []ProgressItem) error {
	shows := make([]ShowEntry, len(items))
	for i, item := range items {
		episodes := make([]Episode, item.Episode)
		for e := 0; e < item.Episode; e++ {
			episodes[e] = Episode{Number: e + 1}
		}
		shows[i] = ShowEntry{Ids: Ids{Anilist: strconv.Itoa(item.AnilistID)}, Episodes: episodes}
	}
	res, err := c.do(ctx, http.MethodPost, "/sync/history", animeEnvelope{Anime: shows})
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return nil
}

func (c *APIClient) RemoveEntry(ctx context.Context, anilistID int) error {
	return c.RemoveEntryBatch(ctx, []int{anilistID})
}

func (c *APIClient) RemoveEntryBatch(ctx context.Context, anilistIDs []int) error {
	shows := make([]ShowEntry, len(anilistIDs))
	for i, id := range anilistIDs {
		shows[i] = ShowEntry{Ids: Ids{Anilist: strconv.Itoa(id)}}
	}
	res, err := c.do(ctx, http.MethodPost, "/sync/history/remove", animeEnvelope{Anime: shows})
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return nil
}

func (c *APIClient) SetRating(ctx context.Context, anilistID int, rating int) error {
	return c.SetRatingBatch(ctx, []RatingItem{{AnilistID: anilistID, Rating: rating}})
}

func (c *APIClient) SetRatingBatch(ctx context.Context, items []RatingItem) error {
	shows := make([]ShowEntry, len(items))
	for i, item := range items {
		shows[i] = ShowEntry{Ids: Ids{Anilist: strconv.Itoa(item.AnilistID)}, Rating: item.Rating}
	}
	res, err := c.do(ctx, http.MethodPost, "/sync/ratings", animeEnvelope{Anime: shows})
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return nil
}

func (c *APIClient) RemoveRating(ctx context.Context, anilistID int) error {
	return c.RemoveRatingBatch(ctx, []int{anilistID})
}

func (c *APIClient) RemoveRatingBatch(ctx context.Context, anilistIDs []int) error {
	shows := make([]ShowEntry, len(anilistIDs))
	for i, id := range anilistIDs {
		shows[i] = ShowEntry{Ids: Ids{Anilist: strconv.Itoa(id)}}
	}
	res, err := c.do(ctx, http.MethodPost, "/sync/ratings/remove", animeEnvelope{Anime: shows})
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return nil
}

func (c *APIClient) TestConnection(ctx context.Context) error {
	res, err := c.do(ctx, http.MethodGet, "/users/settings", nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return nil
}
