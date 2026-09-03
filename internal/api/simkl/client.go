package simkl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// Client is the interface MirroringPlatform depends on, so tests can substitute a fake
// instead of hitting the network.
type Client interface {
	AddToList(ctx context.Context, anilistID int, status string) error
	MarkProgress(ctx context.Context, anilistID int, episode int) error
	RemoveEntry(ctx context.Context, anilistID int) error
	SetRating(ctx context.Context, anilistID int, rating int) error
	RemoveRating(ctx context.Context, anilistID int) error
	TestConnection(ctx context.Context) error
}

type APIClient struct {
	httpClient  *http.Client
	accessToken string
	baseURL     string
}

func NewAPIClient(httpClient *http.Client, accessToken string) *APIClient {
	return &APIClient{httpClient: httpClient, accessToken: accessToken, baseURL: defaultBaseURL}
}

func (c *APIClient) do(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
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

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

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

func (c *APIClient) AddToList(ctx context.Context, anilistID int, status string) error {
	body := animeEnvelopeWithTo{Anime: []ShowEntry{{
		Ids: Ids{Anilist: strconv.Itoa(anilistID)},
		To:  status,
	}}}
	res, err := c.do(ctx, http.MethodPost, "/sync/add-to-list", body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return nil
}

func (c *APIClient) MarkProgress(ctx context.Context, anilistID int, episode int) error {
	episodes := make([]Episode, episode)
	for i := 0; i < episode; i++ {
		episodes[i] = Episode{Number: i + 1}
	}
	body := animeEnvelope{Anime: []ShowEntry{{
		Ids:      Ids{Anilist: strconv.Itoa(anilistID)},
		Episodes: episodes,
	}}}
	res, err := c.do(ctx, http.MethodPost, "/sync/history", body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return nil
}

func (c *APIClient) RemoveEntry(ctx context.Context, anilistID int) error {
	body := animeEnvelope{Anime: []ShowEntry{{
		Ids: Ids{Anilist: strconv.Itoa(anilistID)},
	}}}
	res, err := c.do(ctx, http.MethodPost, "/sync/history/remove", body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return nil
}

func (c *APIClient) SetRating(ctx context.Context, anilistID int, rating int) error {
	body := animeEnvelope{Anime: []ShowEntry{{
		Ids:    Ids{Anilist: strconv.Itoa(anilistID)},
		Rating: rating,
	}}}
	res, err := c.do(ctx, http.MethodPost, "/sync/ratings", body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return nil
}

func (c *APIClient) RemoveRating(ctx context.Context, anilistID int) error {
	body := animeEnvelope{Anime: []ShowEntry{{
		Ids: Ids{Anilist: strconv.Itoa(anilistID)},
	}}}
	res, err := c.do(ctx, http.MethodPost, "/sync/ratings/remove", body)
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
