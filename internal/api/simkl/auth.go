package simkl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

const defaultBaseURL = "https://api.simkl.com"

type pinResponseWire struct {
	Result          string `json:"result"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// RequestPin starts SIMKL's PIN/device authorization flow: display UserCode to the user,
// send them to VerificationURI, then poll PollPin every Interval seconds until ExpiresIn
// elapses or the user approves.
func RequestPin(ctx context.Context, httpClient *http.Client, clientID string) (*PinResponse, error) {
	return requestPinAt(ctx, httpClient, defaultBaseURL, clientID)
}

func requestPinAt(ctx context.Context, httpClient *http.Client, baseURL, clientID string) (*PinResponse, error) {
	endpoint := fmt.Sprintf("%s/oauth/pin?client_id=%s", baseURL, url.QueryEscape(clientID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("simkl: unexpected status %d requesting pin", res.StatusCode)
	}

	var wire pinResponseWire
	if err := json.NewDecoder(res.Body).Decode(&wire); err != nil {
		return nil, err
	}
	return &PinResponse{
		UserCode:        wire.UserCode,
		VerificationURI: wire.VerificationURI,
		ExpiresIn:       wire.ExpiresIn,
		Interval:        wire.Interval,
	}, nil
}

type pinPollResponseWire struct {
	Result      string `json:"result"`
	AccessToken string `json:"access_token"`
}

// PollPin checks whether the user has approved the PIN yet. done is false while the user
// still hasn't entered the code (keep polling every PinResponse.Interval seconds); once
// done is true, accessToken is the token to persist.
func PollPin(ctx context.Context, httpClient *http.Client, clientID, userCode string) (accessToken string, done bool, err error) {
	return pollPinAt(ctx, httpClient, defaultBaseURL, clientID, userCode)
}

func pollPinAt(ctx context.Context, httpClient *http.Client, baseURL, clientID, userCode string) (accessToken string, done bool, err error) {
	// userCode reaches here straight from a request body, so it must be escaped: an
	// unescaped "../" or "?" would otherwise rewrite the path SIMKL is asked for.
	endpoint := fmt.Sprintf("%s/oauth/pin/%s?client_id=%s", baseURL, url.PathEscape(userCode), url.QueryEscape(clientID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", false, err
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return "", false, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("simkl: unexpected status %d polling pin", res.StatusCode)
	}

	var wire pinPollResponseWire
	if err := json.NewDecoder(res.Body).Decode(&wire); err != nil {
		return "", false, err
	}
	if wire.Result == "OK" && wire.AccessToken != "" {
		return wire.AccessToken, true, nil
	}
	return "", false, nil
}
