package mediastream

import (
	"errors"
	"fmt"
	"seanime/internal/mediastream/videofile"
	"strings"
	"seanime/internal/util/result"

	"github.com/rs/zerolog"
)

const (
	StreamTypeTranscode StreamType = "transcode" // On-the-fly transcoding
	StreamTypeOptimized StreamType = "optimized" // Pre-transcoded
	StreamTypeDirect    StreamType = "direct"    // Direct streaming
)

type (
	StreamType string

	PlaybackManager struct {
		logger           *zerolog.Logger
		clientContainers *result.Map[string, *MediaContainer] // Per-client active media containers, keyed by clientId.
		repository       *Repository
		mediaContainers  *result.Map[string, *MediaContainer] // Cache for media containers, keyed by hash.
	}

	PlaybackState struct {
		MediaId int `json:"mediaId"` // The media ID
	}

	MediaContainer struct {
		Filepath   string               `json:"filePath"`
		Hash       string               `json:"hash"`
		StreamType StreamType           `json:"streamType"` // Tells the frontend how to play the media.
		StreamUrl  string               `json:"streamUrl"`  // The relative endpoint to stream the media.
		MediaInfo  *videofile.MediaInfo `json:"mediaInfo"`
		//Metadata  *Metadata       `json:"metadata"`
		// todo: add more fields (e.g. metadata)
	}
)

func NewPlaybackManager(repository *Repository) *PlaybackManager {
	return &PlaybackManager{
		logger:           repository.logger,
		repository:       repository,
		clientContainers: result.NewMap[string, *MediaContainer](),
		mediaContainers:  result.NewMap[string, *MediaContainer](),
	}
}

func (p *PlaybackManager) KillPlayback(clientId string) {
	p.logger.Debug().Str("clientId", clientId).Msg("mediastream: Killing playback for client")
	p.clientContainers.Delete(clientId)
}

func (p *PlaybackManager) KillAllPlayback() {
	p.logger.Debug().Msg("mediastream: Killing all playback")
	p.clientContainers.Clear()
}

// RequestPlayback is called by the frontend to stream a media file
func (p *PlaybackManager) RequestPlayback(filepath string, streamType StreamType, clientId string) (ret *MediaContainer, err error) {

	p.logger.Debug().Str("filepath", filepath).Str("clientId", clientId).Any("type", streamType).Msg("mediastream: Requesting playback")

	// Create a new media container
	ret, err = p.newMediaContainer(filepath, streamType)

	if err != nil {
		p.logger.Error().Err(err).Msg("mediastream: Failed to create media container")
		return nil, fmt.Errorf("failed to create media container: %v", err)
	}

	// Create a client-specific copy with clientId in the stream URL
	clientContainer := *ret
	if clientContainer.StreamType == StreamTypeTranscode && !strings.Contains(clientContainer.StreamUrl, "clientId=") {
		if strings.Contains(clientContainer.StreamUrl, "?") {
			clientContainer.StreamUrl += "&clientId=" + clientId
		} else {
			clientContainer.StreamUrl += "?clientId=" + clientId
		}
	}

	// Store the media container for this client.
	p.clientContainers.Set(clientId, &clientContainer)

	p.logger.Info().Str("filepath", filepath).Str("clientId", clientId).Msg("mediastream: Ready to play media")

	ret = &clientContainer
	return
}

// GetMediaContainer returns the media container for the given client, or false if not found.
func (p *PlaybackManager) GetMediaContainer(clientId string) (*MediaContainer, bool) {
	return p.clientContainers.Get(clientId)
}

// HasActiveSessions returns true if any client has an active media container.
func (p *PlaybackManager) HasActiveSessions() bool {
	count := 0
	p.clientContainers.Range(func(_ string, _ *MediaContainer) bool {
		count++
		return false // stop after first
	})
	return count > 0
}

// PreloadPlayback is called by the frontend to preload a media container so that the data is stored in advanced
func (p *PlaybackManager) PreloadPlayback(filepath string, streamType StreamType) (ret *MediaContainer, err error) {

	p.logger.Debug().Str("filepath", filepath).Any("type", streamType).Msg("mediastream: Preloading playback")

	// Create a new media container
	ret, err = p.newMediaContainer(filepath, streamType)

	if err != nil {
		p.logger.Error().Err(err).Msg("mediastream: Failed to create media container")
		return nil, fmt.Errorf("failed to create media container: %v", err)
	}

	p.logger.Info().Str("filepath", filepath).Msg("mediastream: Ready to play media")

	return
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
// Optimize
//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

func (p *PlaybackManager) newMediaContainer(filepath string, streamType StreamType) (ret *MediaContainer, err error) {
	p.logger.Debug().Str("filepath", filepath).Any("type", streamType).Msg("mediastream: New media container requested")
	// Get the hash of the file.
	hash, err := videofile.GetHashFromPath(filepath)
	if err != nil {
		return nil, err
	}

	p.logger.Trace().Str("hash", hash).Msg("mediastream: Checking cache")

	// Check the cache ONLY if the stream type is the same.
	if mc, ok := p.mediaContainers.Get(hash); ok && mc.StreamType == streamType {
		p.logger.Debug().Str("hash", hash).Msg("mediastream: Media container cache HIT")
		return mc, nil
	}

	p.logger.Trace().Str("hash", hash).Msg("mediastream: Creating media container")

	// Get the media information of the file.
	ret = &MediaContainer{
		Filepath:   filepath,
		Hash:       hash,
		StreamType: streamType,
	}

	p.logger.Debug().Msg("mediastream: Extracting media info")

	ret.MediaInfo, err = p.repository.mediaInfoExtractor.GetInfo(p.repository.settings.MustGet().FfprobePath, filepath)
	if err != nil {
		return nil, err
	}

	// Skip attachment extraction for remote URLs (too slow for large files)
	if !strings.HasPrefix(filepath, "http://") && !strings.HasPrefix(filepath, "https://") {
		p.logger.Debug().Msg("mediastream: Extracted media info, extracting attachments")

		err = videofile.ExtractAttachment(p.repository.settings.MustGet().FfmpegPath, filepath, hash, ret.MediaInfo, p.repository.cacheDir, p.logger)
		if err != nil {
			p.logger.Error().Err(err).Msg("mediastream: Failed to extract attachments")
			return nil, err
		}

		p.logger.Debug().Msg("mediastream: Extracted attachments")
	} else {
		p.logger.Debug().Msg("mediastream: Skipping attachment extraction for remote URL")
	}

	streamUrl := ""
	switch streamType {
	case StreamTypeDirect:
		// Directly serve the file.
		streamUrl = "/api/v1/mediastream/direct"
	case StreamTypeTranscode:
		// Live transcode the file.
		streamUrl = "/api/v1/mediastream/transcode/master.m3u8"
	case StreamTypeOptimized:
		// TODO: Check if the file is already transcoded when the feature is implemented.
		// ...
		streamUrl = "/api/v1/mediastream/hls/master.m3u8"
	}

	// TODO: Add metadata to the media container.
	// ...

	if streamUrl == "" {
		return nil, errors.New("invalid stream type")
	}

	// Set the stream URL.
	ret.StreamUrl = streamUrl

	// Store the media container in the map.
	p.mediaContainers.Set(hash, ret)

	return
}
