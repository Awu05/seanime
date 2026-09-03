package simkl

// Ids identifies a media item to SIMKL. Anilist is always set by this client since every
// SIMKL sync write endpoint accepts an AniList ID directly - no separate lookup call needed.
type Ids struct {
	Simkl   int    `json:"simkl,omitempty"`
	Anilist string `json:"anilist,omitempty"`
}

type Episode struct {
	Number int `json:"number"`
}

type Season struct {
	Number   int       `json:"number"`
	Episodes []Episode `json:"episodes,omitempty"`
}

// ShowEntry is the per-item shape used across SIMKL's anime sync endpoints. Which fields
// are set depends on the operation: To for /sync/add-to-list, Episodes for /sync/history,
// Rating for /sync/ratings. Anime entries always go under the top-level "anime" array key.
type ShowEntry struct {
	Ids      Ids       `json:"ids"`
	To       string    `json:"to,omitempty"`
	Episodes []Episode `json:"episodes,omitempty"`
	Seasons  []Season  `json:"seasons,omitempty"`
	Rating   int       `json:"rating,omitempty"`
}

// animeEnvelope is the body shape for every anime sync endpoint. Which per-item fields are
// set is what varies by operation - for /sync/add-to-list the concrete status is nested
// per-item in ShowEntry.To (confirmed against SIMKL's own add_anime_completed/dropped/hold/
// plantowatch examples), not a single top-level field.
type animeEnvelope struct {
	Anime []ShowEntry `json:"anime"`
}

// PinResponse is the response from GET /oauth/pin (step 1 of SIMKL's PIN/device flow).
type PinResponse struct {
	UserCode        string `json:"userCode"`
	VerificationURI string `json:"verificationUri"`
	ExpiresIn       int    `json:"expiresIn"`
	Interval        int    `json:"interval"`
}
