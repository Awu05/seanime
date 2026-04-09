package stremthru

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"seanime/internal/constants"
	"seanime/internal/debrid/debrid"
	"seanime/internal/util"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/samber/mo"
)

type (
	StremThru struct {
		baseUrl   string
		storeName string
		apiKey    mo.Option[string]
		client    *http.Client
		logger    *zerolog.Logger
	}

	Response struct {
		Data interface{} `json:"data"`
	}

	ErrorResponse struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	MagnetFile struct {
		Index     int    `json:"index"`
		Link      string `json:"link"`
		Name      string `json:"name"`
		Path      string `json:"path"`
		Size      int64  `json:"size"`
		VideoHash string `json:"video_hash"`
	}

	Magnet struct {
		ID      string        `json:"id"`
		Hash    string        `json:"hash"`
		Magnet  string        `json:"magnet"`
		Name    string        `json:"name"`
		Size    int64         `json:"size"`
		Status  string        `json:"status"`
		Private bool          `json:"private"`
		Files   []*MagnetFile `json:"files"`
		AddedAt string        `json:"added_at"`
	}

	MagnetListItem struct {
		ID      string `json:"id"`
		Hash    string `json:"hash"`
		Name    string `json:"name"`
		Size    int64  `json:"size"`
		Status  string `json:"status"`
		Private bool   `json:"private"`
		AddedAt string `json:"added_at"`
	}

	MagnetListResponse struct {
		Items      []*MagnetListItem `json:"items"`
		TotalItems int               `json:"total_items"`
	}

	MagnetCheckItem struct {
		Hash   string        `json:"hash"`
		Magnet string        `json:"magnet"`
		Status string        `json:"status"`
		Files  []*MagnetFile `json:"files"`
	}

	MagnetCheckResponse struct {
		Items []*MagnetCheckItem `json:"items"`
	}

	LinkResponse struct {
		Link string `json:"link"`
	}

	UserResponse struct {
		ID                 string `json:"id"`
		Email              string `json:"email"`
		SubscriptionStatus string `json:"subscription_status"`
	}
)

func NewStremThru(logger *zerolog.Logger, apiUrl string, storeName string) debrid.Provider {
	baseUrl := strings.TrimRight(apiUrl, "/")
	if baseUrl == "" {
		baseUrl = "http://localhost:8080"
	}

	return &StremThru{
		baseUrl:   baseUrl,
		storeName: storeName,
		apiKey:    mo.None[string](),
		client: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 5,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		logger: logger,
	}
}

func (s *StremThru) GetSettings() debrid.Settings {
	return debrid.Settings{
		ID:   "stremthru",
		Name: "StremThru",
	}
}

func (s *StremThru) doQuery(method, uri string, body io.Reader, contentType string) (*Response, error) {
	return s.doQueryCtx(context.Background(), method, uri, body, contentType)
}

func (s *StremThru) doQueryCtx(ctx context.Context, method, uri string, body io.Reader, contentType string) (*Response, error) {
	apiKey, found := s.apiKey.Get()
	if !found {
		return nil, debrid.ErrNotAuthenticated
	}

	req, err := http.NewRequestWithContext(ctx, method, uri, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("X-StremThru-Authorization", "Basic "+apiKey)
	if s.storeName != "" {
		req.Header.Set("X-StremThru-Store-Name", s.storeName)
	}
	req.Header.Set("User-Agent", "Seanime/"+constants.Version)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyB, err := io.ReadAll(resp.Body)
	if err != nil {
		s.logger.Error().Err(err).Msg("stremthru: Failed to read response body")
		return nil, err
	}

	if resp.StatusCode >= 400 {
		var errResp ErrorResponse
		if json.Unmarshal(bodyB, &errResp) == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("stremthru: %s", errResp.Error.Message)
		}
		return nil, fmt.Errorf("stremthru: request failed with status %d: %s", resp.StatusCode, string(bodyB))
	}

	var ret Response
	if err := json.Unmarshal(bodyB, &ret); err != nil {
		trimmedBody := string(bodyB)
		if len(trimmedBody) > 2000 {
			trimmedBody = trimmedBody[:2000] + "..."
		}
		s.logger.Error().Err(err).Msg("stremthru: Failed to decode response, body: " + trimmedBody)
		return nil, err
	}

	return &ret, nil
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// Authenticate stores the credentials for Basic auth.
// Matches JS SDK: new StremThru({ auth: "user:pass" })
func (s *StremThru) Authenticate(apiKey string) error {
	apiKey = strings.TrimSpace(apiKey)
	if strings.Contains(apiKey, ":") {
		apiKey = base64.StdEncoding.EncodeToString([]byte(apiKey))
	}
	s.apiKey = mo.Some(apiKey)
	s.logger.Info().Str("baseUrl", s.baseUrl).Msg("stremthru: Credentials set")
	return nil
}

// GetInstantAvailability checks magnet availability.
// Matches JS SDK: store.checkMagnet({ magnet: [...] })
// Endpoint: GET /v0/store/magnets/check?magnet=hash1,hash2
func (s *StremThru) GetInstantAvailability(hashes []string) map[string]debrid.TorrentItemInstantAvailability {
	s.logger.Trace().Strs("hashes", hashes).Msg("stremthru: Checking instant availability")

	availability := make(map[string]debrid.TorrentItemInstantAvailability)

	if len(hashes) == 0 {
		return availability
	}

	var hashBatches [][]string
	for i := 0; i < len(hashes); i += 100 {
		end := i + 100
		if end > len(hashes) {
			end = len(hashes)
		}
		hashBatches = append(hashBatches, hashes[i:end])
	}

	for _, batch := range hashBatches {
		resp, err := s.doQuery("GET", s.baseUrl+"/v0/store/magnets/check?magnet="+strings.Join(batch, ","), nil, "")
		if err != nil {
			s.logger.Error().Err(err).Msg("stremthru: Failed to check instant availability")
			return availability
		}

		marshaledData, _ := json.Marshal(resp.Data)

		var checkResp MagnetCheckResponse
		if err := json.Unmarshal(marshaledData, &checkResp); err != nil {
			s.logger.Error().Err(err).Msg("stremthru: Failed to parse check response")
			return availability
		}

		for _, item := range checkResp.Items {
			if item.Status != "cached" {
				continue
			}
			availability[item.Hash] = debrid.TorrentItemInstantAvailability{
				CachedFiles: make(map[string]*debrid.CachedFile),
			}
			for _, file := range item.Files {
				availability[item.Hash].CachedFiles[strconv.Itoa(file.Index)] = &debrid.CachedFile{
					Name: file.Name,
					Size: file.Size,
				}
			}
		}
	}

	return availability
}

// AddTorrent adds a magnet link.
// Matches JS SDK: store.addMagnet({ magnet: "..." })
// Endpoint: POST /v0/store/magnets
func (s *StremThru) AddTorrent(opts debrid.AddTorrentOptions) (string, error) {
	s.logger.Trace().Str("magnetLink", opts.MagnetLink).Msg("stremthru: Adding magnet")

	// Check if already added
	if opts.InfoHash != "" {
		magnets, err := s.listMagnets()
		if err == nil {
			for _, m := range magnets {
				if strings.EqualFold(m.Hash, opts.InfoHash) {
					return m.ID, nil
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	payload, _ := json.Marshal(map[string]string{"magnet": opts.MagnetLink})

	resp, err := s.doQuery("POST", s.baseUrl+"/v0/store/magnets", strings.NewReader(string(payload)), "application/json")
	if err != nil {
		return "", fmt.Errorf("stremthru: Failed to add magnet: %w", err)
	}

	marshaledData, _ := json.Marshal(resp.Data)
	var magnet Magnet
	if err := json.Unmarshal(marshaledData, &magnet); err != nil {
		return "", fmt.Errorf("stremthru: Failed to parse add magnet response: %w", err)
	}

	s.logger.Debug().Str("magnetId", magnet.ID).Str("name", magnet.Name).Str("hash", magnet.Hash).Msg("stremthru: Magnet added")
	return magnet.ID, nil
}

// GetTorrentStreamUrl blocks until the magnet is ready and returns the stream URL.
// Matches JS SDK: store.getMagnet(id) + store.generateLink({ link })
func (s *StremThru) GetTorrentStreamUrl(ctx context.Context, opts debrid.StreamTorrentOptions, itemCh chan debrid.TorrentItem) (streamUrl string, err error) {
	s.logger.Trace().Str("magnetId", opts.ID).Str("fileId", opts.FileId).Msg("stremthru: Retrieving stream link")

	doneCh := make(chan struct{})

	go func(ctx context.Context) {
		defer close(doneCh)

		for {
			select {
			case <-ctx.Done():
				err = ctx.Err()
				return
			case <-time.After(4 * time.Second):
				torrent, _err := s.GetTorrent(opts.ID)
				if _err != nil {
					s.logger.Error().Err(_err).Msg("stremthru: Failed to get magnet")
					err = fmt.Errorf("stremthru: Failed to get magnet: %w", _err)
					return
				}

				itemCh <- *torrent

				if torrent.IsReady {
					time.Sleep(1 * time.Second)
					downloadUrl, _err := s.GetTorrentDownloadUrl(debrid.DownloadTorrentOptions{
						ID:     opts.ID,
						FileId: opts.FileId,
					})
					if _err != nil {
						s.logger.Error().Err(_err).Msg("stremthru: Failed to get download URL")
						err = _err
						return
					}

					streamUrl = downloadUrl
					return
				}
			}
		}
	}(ctx)

	<-doneCh
	return
}

// GetTorrentDownloadUrl generates a download link for a magnet file.
// Matches JS SDK: store.generateLink({ link: fileLink })
// Endpoint: POST /v0/store/link/generate
func (s *StremThru) GetTorrentDownloadUrl(opts debrid.DownloadTorrentOptions) (downloadUrl string, err error) {
	s.logger.Trace().Str("magnetId", opts.ID).Msg("stremthru: Retrieving download link")

	// Get the magnet to find the file link
	magnet, err := s.getMagnet(opts.ID)
	if err != nil {
		return "", fmt.Errorf("stremthru: Failed to get magnet for download: %w", err)
	}

	var fileLink string
	if opts.FileId != "" {
		// Find the file by name
		for _, f := range magnet.Files {
			if f.Name == opts.FileId {
				fileLink = f.Link
				break
			}
		}
		// Try by index
		if fileLink == "" {
			idx, convErr := strconv.Atoi(opts.FileId)
			if convErr == nil {
				for _, f := range magnet.Files {
					if f.Index == idx {
						fileLink = f.Link
						break
					}
				}
			}
		}
	}

	// Fallback to first file
	if fileLink == "" && len(magnet.Files) > 0 {
		fileLink = magnet.Files[0].Link
	}

	if fileLink == "" {
		return "", fmt.Errorf("stremthru: No files available for download")
	}

	// Generate download link
	// Matches JS SDK: store.generateLink({ link })
	// Endpoint: POST /v0/store/link/generate
	payload, _ := json.Marshal(map[string]string{"link": fileLink})
	resp, err := s.doQuery("POST", s.baseUrl+"/v0/store/link/generate", strings.NewReader(string(payload)), "application/json")
	if err != nil {
		return "", fmt.Errorf("stremthru: Failed to generate download link: %w", err)
	}

	marshaledData, _ := json.Marshal(resp.Data)
	var linkResp LinkResponse
	if err := json.Unmarshal(marshaledData, &linkResp); err != nil {
		return "", fmt.Errorf("stremthru: Failed to parse download link response: %w", err)
	}

	s.logger.Debug().Str("downloadUrl", linkResp.Link).Msg("stremthru: Download link retrieved")
	return linkResp.Link, nil
}

// GetTorrent gets a single magnet by ID.
// Matches JS SDK: store.getMagnet(id)
// Endpoint: GET /v0/store/magnets/{id}
func (s *StremThru) GetTorrent(id string) (ret *debrid.TorrentItem, err error) {
	magnet, err := s.getMagnet(id)
	if err != nil {
		return nil, err
	}
	return toDebridTorrent(magnet), nil
}

func (s *StremThru) getMagnet(id string) (*Magnet, error) {
	resp, err := s.doQuery("GET", s.baseUrl+"/v0/store/magnets/"+id, nil, "")
	if err != nil {
		return nil, fmt.Errorf("stremthru: Failed to get magnet: %w", err)
	}

	marshaledData, _ := json.Marshal(resp.Data)
	var magnet Magnet
	if err := json.Unmarshal(marshaledData, &magnet); err != nil {
		return nil, fmt.Errorf("stremthru: Failed to parse magnet: %w", err)
	}

	return &magnet, nil
}

// GetTorrentInfo uses checkMagnet to get file info.
// Matches JS SDK: store.checkMagnet({ magnet: [hash] })
// Endpoint: GET /v0/store/magnets/check?magnet=hash
func (s *StremThru) GetTorrentInfo(opts debrid.GetTorrentInfoOptions) (ret *debrid.TorrentInfo, err error) {
	if opts.InfoHash == "" {
		return nil, fmt.Errorf("stremthru: No info hash provided")
	}

	resp, err := s.doQuery("GET", s.baseUrl+"/v0/store/magnets/check?magnet="+opts.InfoHash, nil, "")
	if err != nil {
		return nil, fmt.Errorf("stremthru: Failed to check magnet: %w", err)
	}

	marshaledData, _ := json.Marshal(resp.Data)
	var checkResp MagnetCheckResponse
	if err := json.Unmarshal(marshaledData, &checkResp); err != nil {
		return nil, fmt.Errorf("stremthru: Failed to parse check response: %w", err)
	}

	for _, item := range checkResp.Items {
		if strings.EqualFold(item.Hash, opts.InfoHash) {
			return toDebridTorrentInfoFromCheck(item), nil
		}
	}

	// If not found in check, try adding and getting info
	if opts.MagnetLink != "" {
		id, err := s.AddTorrent(debrid.AddTorrentOptions{
			MagnetLink: opts.MagnetLink,
			InfoHash:   opts.InfoHash,
		})
		if err != nil {
			return nil, fmt.Errorf("stremthru: Failed to get torrent info: %w", err)
		}

		magnet, err := s.getMagnet(id)
		if err != nil {
			return nil, fmt.Errorf("stremthru: Failed to get torrent info: %w", err)
		}

		return toDebridTorrentInfoFromMagnet(magnet), nil
	}

	return nil, fmt.Errorf("stremthru: Torrent not found")
}

// GetTorrents lists all magnets.
// Matches JS SDK: store.listMagnets({ limit: 500 })
// Endpoint: GET /v0/store/magnets?limit=500
func (s *StremThru) GetTorrents() (ret []*debrid.TorrentItem, err error) {
	items, err := s.listMagnets()
	if err != nil {
		return nil, fmt.Errorf("stremthru: Failed to get magnets: %w", err)
	}

	for _, item := range items {
		ret = append(ret, toDebridTorrentFromListItem(item))
	}

	slices.SortFunc(ret, func(i, j *debrid.TorrentItem) int {
		return cmp.Compare(j.AddedAt, i.AddedAt)
	})

	return ret, nil
}

func (s *StremThru) listMagnets() ([]*MagnetListItem, error) {
	resp, err := s.doQuery("GET", s.baseUrl+"/v0/store/magnets?limit=500", nil, "")
	if err != nil {
		return nil, fmt.Errorf("stremthru: Failed to list magnets: %w", err)
	}

	marshaledData, _ := json.Marshal(resp.Data)
	var listResp MagnetListResponse
	if err := json.Unmarshal(marshaledData, &listResp); err != nil {
		return nil, fmt.Errorf("stremthru: Failed to parse magnet list: %w", err)
	}

	return listResp.Items, nil
}

// DeleteTorrent removes a magnet by ID.
// Matches JS SDK: store.removeMagnet(id)
// Endpoint: DELETE /v0/store/magnets/{id}
func (s *StremThru) DeleteTorrent(id string) error {
	_, err := s.doQuery("DELETE", s.baseUrl+"/v0/store/magnets/"+id, nil, "")
	if err != nil {
		return fmt.Errorf("stremthru: Failed to delete magnet: %w", err)
	}
	return nil
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

func toDebridTorrent(m *Magnet) *debrid.TorrentItem {
	addedAt, _ := time.Parse(time.RFC3339, m.AddedAt)

	var files []*debrid.TorrentItemFile
	for _, f := range m.Files {
		files = append(files, &debrid.TorrentItemFile{
			ID:    f.Name,
			Index: f.Index,
			Name:  f.Name,
			Path:  f.Path,
			Size:  f.Size,
		})
	}

	status := toDebridTorrentStatus(m.Status)
	isReady := status == debrid.TorrentItemStatusCompleted

	completionPercentage := 0
	if isReady {
		completionPercentage = 100
	}

	return &debrid.TorrentItem{
		ID:                   m.ID,
		Name:                 m.Name,
		Hash:                 m.Hash,
		Size:                 m.Size,
		FormattedSize:        util.Bytes(uint64(m.Size)),
		CompletionPercentage: completionPercentage,
		Status:               status,
		AddedAt:              addedAt.Format(time.RFC3339),
		IsReady:              isReady,
		Files:                files,
	}
}

func toDebridTorrentFromListItem(m *MagnetListItem) *debrid.TorrentItem {
	addedAt, _ := time.Parse(time.RFC3339, m.AddedAt)

	status := toDebridTorrentStatus(m.Status)
	isReady := status == debrid.TorrentItemStatusCompleted

	completionPercentage := 0
	if isReady {
		completionPercentage = 100
	}

	return &debrid.TorrentItem{
		ID:                   m.ID,
		Name:                 m.Name,
		Hash:                 m.Hash,
		Size:                 m.Size,
		FormattedSize:        util.Bytes(uint64(m.Size)),
		CompletionPercentage: completionPercentage,
		Status:               status,
		AddedAt:              addedAt.Format(time.RFC3339),
		IsReady:              isReady,
	}
}

func toDebridTorrentInfoFromCheck(m *MagnetCheckItem) *debrid.TorrentInfo {
	var files []*debrid.TorrentItemFile
	for _, f := range m.Files {
		files = append(files, &debrid.TorrentItemFile{
			ID:    f.Name,
			Index: f.Index,
			Name:  f.Name,
			Path:  f.Path,
			Size:  f.Size,
		})
	}

	return &debrid.TorrentInfo{
		Name:  "",
		Hash:  m.Hash,
		Files: files,
	}
}

func toDebridTorrentInfoFromMagnet(m *Magnet) *debrid.TorrentInfo {
	var files []*debrid.TorrentItemFile
	for _, f := range m.Files {
		files = append(files, &debrid.TorrentItemFile{
			ID:    f.Name,
			Index: f.Index,
			Name:  f.Name,
			Path:  f.Path,
			Size:  f.Size,
		})
	}

	return &debrid.TorrentInfo{
		ID:    &m.ID,
		Name:  m.Name,
		Hash:  m.Hash,
		Size:  m.Size,
		Files: files,
	}
}

func toDebridTorrentStatus(status string) debrid.TorrentItemStatus {
	switch status {
	case "cached", "downloaded":
		return debrid.TorrentItemStatusCompleted
	case "downloading":
		return debrid.TorrentItemStatusDownloading
	case "queued", "processing":
		return debrid.TorrentItemStatusStalled
	case "uploading":
		return debrid.TorrentItemStatusSeeding
	case "failed", "invalid":
		return debrid.TorrentItemStatusError
	default:
		return debrid.TorrentItemStatusOther
	}
}
