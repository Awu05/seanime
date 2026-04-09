package directstream

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"seanime/internal/api/anilist"
	"seanime/internal/library/anime"
	"seanime/internal/mkvparser"
	"seanime/internal/nativeplayer"
	"seanime/internal/util"
	"seanime/internal/util/result"
	"seanime/internal/videocore"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Stream is the common interface for all stream types.
type Stream interface {
	// Type returns the type of the stream.
	Type() nativeplayer.StreamType
	// LoadContentType loads and returns the content type of the stream.
	// e.g. "video/mp4", "video/webm", "video/x-matroska"
	LoadContentType() string
	// ClientId returns the client ID of the current stream.
	ClientId() string
	// Media returns the media of the current stream.
	Media() *anilist.BaseAnime
	// Episode returns the episode of the current stream.
	Episode() *anime.Episode
	// ListEntryData returns the list entry data for the current stream.
	ListEntryData() *anime.EntryListData
	// EpisodeCollection returns the episode collection for the media of the current stream.
	EpisodeCollection() *anime.EpisodeCollection
	// LoadPlaybackInfo loads and returns the playback info.
	LoadPlaybackInfo() (*nativeplayer.PlaybackInfo, error)
	// GetAttachmentByName returns the attachment by name for the stream.
	// It is used to serve fonts and other attachments.
	GetAttachmentByName(filename string) (*mkvparser.AttachmentInfo, bool)
	// GetStreamHandler returns the stream handler.
	GetStreamHandler() http.Handler
	// StreamError is called when an error occurs while streaming.
	// This is used to notify the native player that an error occurred.
	// It will close the stream.
	StreamError(err error)
	// Terminate ends the stream.
	// Once this is called, the stream should not be used anymore.
	Terminate()
	// GetSubtitleEventCache accesses the subtitle event cache.
	GetSubtitleEventCache() *result.Map[string, *mkvparser.SubtitleEvent]
	// OnSubtitleFileUploaded is called when a subtitle file is uploaded.
	OnSubtitleFileUploaded(filename string, content string)
}

func (m *Manager) getStreamHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Look up the stream by clientId query param
		clientId := r.URL.Query().Get("clientId")
		if clientId != "" {
			if stream, ok := m.streams.Get(clientId); ok {
				stream.GetStreamHandler().ServeHTTP(w, r)
				return
			}
		}
		// Fallback: try to find any active stream (backward compatibility for single-stream)
		var activeStream Stream
		m.streams.Range(func(_ string, s Stream) bool {
			activeStream = s
			return false
		})
		if activeStream == nil {
			http.Error(w, "no stream", http.StatusInternalServerError)
			return
		}
		activeStream.GetStreamHandler().ServeHTTP(w, r)
	})
}

func (m *Manager) PrepareNewStream(clientId string, step string) {
	m.prepareNewStream(clientId, step)
}

func (m *Manager) StreamError(err error) {
	// This is a generic error — terminate all streams
	m.streams.Range(func(_ string, stream Stream) bool {
		m.Logger.Warn().Err(err).Msgf("directstream: Terminating stream with error")
		stream.StreamError(err)
		return true
	})
}

func (m *Manager) AbortOpen(clientId string, err error) {
	m.abortPreparation(clientId, err)
}

func (m *Manager) prepareNewStream(clientId string, step string) {
	// Terminate only the previous stream for THIS client (if any)
	if existing, ok := m.streams.Get(clientId); ok {
		m.Logger.Debug().Str("clientId", clientId).Msg("directstream: Terminating previous stream for client")
		existing.Terminate()
		m.streams.Delete(clientId)
	}

	m.Logger.Debug().Str("clientId", clientId).Msg("directstream: Signaling native player that a new stream is starting")
	m.nativePlayer.OpenAndAwait(clientId, step)
}

func (m *Manager) abortPreparation(clientId string, err error) {
	// Terminate only this client's stream
	if existing, ok := m.streams.Get(clientId); ok {
		m.Logger.Debug().Str("clientId", clientId).Msg("directstream: Terminating stream before abort")
		existing.Terminate()
		m.streams.Delete(clientId)
	}

	m.Logger.Debug().Str("clientId", clientId).Msgf("directstream: Signaling native player to abort stream preparation, reason: %s", err.Error())
	m.nativePlayer.AbortOpen(clientId, err.Error())
}

// loadStream loads a new stream for a client. Does not affect other clients' streams.
func (m *Manager) loadStream(stream Stream) {
	clientId := stream.ClientId()
	m.prepareNewStream(clientId, "Loading stream...")

	m.Logger.Debug().Str("clientId", clientId).Msg("directstream: Loading stream")

	// Create a per-stream context
	ctx, cancel := context.WithCancel(context.Background())
	if bs, ok := m.getBaseStream(stream); ok {
		bs.streamCtx = ctx
		bs.streamCancelFunc = cancel
	}

	// Register the stream
	m.streams.Set(clientId, stream)

	m.Logger.Debug().Str("clientId", clientId).Msg("directstream: Loading content type")
	m.nativePlayer.OpenAndAwait(clientId, "Loading metadata...")

	contentType := stream.LoadContentType()
	if contentType == "" {
		m.Logger.Error().Str("clientId", clientId).Msg("directstream: Failed to load content type")
		m.preStreamError(stream, fmt.Errorf("failed to load content type"))
		return
	}

	m.Logger.Debug().Str("clientId", clientId).Msg("directstream: Signaling native player that metadata is being loaded")

	playbackInfo, err := stream.LoadPlaybackInfo()
	if err != nil {
		m.Logger.Error().Err(err).Str("clientId", clientId).Msg("directstream: Failed to load playback info")
		m.preStreamError(stream, fmt.Errorf("failed to load playback info: %w", err))
		return
	}

	m.Logger.Debug().Str("clientId", clientId).Msg("directstream: Signaling native player that stream is ready")
	m.nativePlayer.Watch(clientId, playbackInfo)
}

// getBaseStream extracts the BaseStream from any Stream implementation.
func (m *Manager) getBaseStream(s Stream) (*BaseStream, bool) {
	switch v := s.(type) {
	case *DebridStream:
		return &v.BaseStream, true
	case *TorrentStream:
		return &v.BaseStream, true
	case *LocalFileStream:
		return &v.BaseStream, true
	case *Nakama:
		return &v.BaseStream, true
	case *BaseStream:
		return v, true
	default:
		return nil, false
	}
}

func (m *Manager) listenToPlayerEvents() {
	go func() {
		defer func() {
			m.Logger.Trace().Msg("directstream: Stream loop goroutine exited")
		}()

		for {
			select {
			case event := <-m.videoCoreSubscriber.Events():
				if !event.IsNativePlayer() {
					continue
				}

				// Look up the stream by the event's clientId
				cs, ok := m.streams.Get(event.GetClientId())
				if !ok {
					continue
				}

				switch event := event.(type) {
				case *videocore.VideoLoadedMetadataEvent:
					m.Logger.Debug().Str("clientId", cs.ClientId()).Msg("directstream: Video loaded metadata")
					if lfStream, ok := cs.(*LocalFileStream); ok {
						subReader, err := lfStream.newReader()
						if err != nil {
							m.Logger.Error().Err(err).Msg("directstream: Failed to create subtitle reader")
							cs.StreamError(fmt.Errorf("failed to create subtitle reader: %w", err))
							continue
						}
						lfStream.StartSubtitleStream(lfStream, lfStream.streamCtx, subReader, 0)
					} else if ts, ok := cs.(*TorrentStream); ok {
						subReader := ts.file.NewReader()
						subReader.SetResponsive()
						ts.StartSubtitleStream(ts, ts.streamCtx, subReader, 0)
					}
				case *videocore.VideoErrorEvent:
					m.Logger.Debug().Str("clientId", cs.ClientId()).Msgf("directstream: Video error, Error: %s", event.Error)
					cs.StreamError(fmt.Errorf(event.Error))
				case *videocore.SubtitleFileUploadedEvent:
					m.Logger.Debug().Str("clientId", cs.ClientId()).Msgf("directstream: Subtitle file uploaded, Filename: %s", event.Filename)
					cs.OnSubtitleFileUploaded(event.Filename, event.Content)
				case *videocore.VideoTerminatedEvent:
					m.Logger.Debug().Str("clientId", cs.ClientId()).Msg("directstream: Video terminated")
					cs.Terminate()
					m.streams.Delete(cs.ClientId())
				case *videocore.VideoCompletedEvent:
					m.Logger.Debug().Str("clientId", cs.ClientId()).Msg("directstream: Video completed")

					if bs, ok := m.getBaseStream(cs); ok {
						bs.updateProgress.Do(func() {
							mediaId := bs.media.GetID()
							epNum := bs.episode.GetProgressNumber()
							totalEpisodes := bs.media.GetTotalEpisodeCount()

							_ = bs.manager.platformRef.Get().UpdateEntryProgress(context.Background(), mediaId, epNum, &totalEpisodes)
						})
					}
				}
			}
		}
	}()
}

// unloadStreamByClientId removes and terminates a specific client's stream.
func (m *Manager) unloadStreamByClientId(clientId string) {
	m.Logger.Debug().Str("clientId", clientId).Msg("directstream: Unloading stream for client")

	if stream, ok := m.streams.Get(clientId); ok {
		stream.Terminate()
		m.streams.Delete(clientId)
	}

	m.Logger.Debug().Str("clientId", clientId).Msg("directstream: Stream unloaded successfully")
}

///////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type BaseStream struct {
	logger                 *zerolog.Logger
	clientId               string
	contentType            string
	contentTypeOnce        sync.Once
	episode                *anime.Episode
	media                  *anilist.BaseAnime
	listEntryData          *anime.EntryListData
	episodeCollection      *anime.EpisodeCollection
	playbackInfo           *nativeplayer.PlaybackInfo
	playbackInfoErr        error
	playbackInfoOnce       sync.Once
	subtitleEventCache     *result.Map[string, *mkvparser.SubtitleEvent]
	terminateOnce          sync.Once
	serveContentCancelFunc context.CancelFunc
	filename               string // Name of the file being streamed, if applicable

	// Per-stream context — each stream has its own lifecycle context
	streamCtx        context.Context
	streamCancelFunc context.CancelFunc

	// Subtitle stream management
	activeSubtitleStreams *result.Map[string, *SubtitleStream]

	manager        *Manager
	updateProgress sync.Once
}

// StreamCtx returns this stream's context. Use this instead of manager.playbackCtx.
func (s *BaseStream) StreamCtx() context.Context {
	return s.streamCtx
}

var _ Stream = (*BaseStream)(nil)

func (s *BaseStream) GetAttachmentByName(filename string) (*mkvparser.AttachmentInfo, bool) {
	return nil, false
}

func (s *BaseStream) GetStreamHandler() http.Handler {
	return nil
}

func (s *BaseStream) LoadContentType() string {
	return s.contentType
}

func (s *BaseStream) LoadPlaybackInfo() (*nativeplayer.PlaybackInfo, error) {
	return s.playbackInfo, s.playbackInfoErr
}

func (s *BaseStream) Type() nativeplayer.StreamType {
	return ""
}

func (s *BaseStream) Media() *anilist.BaseAnime {
	return s.media
}

func (s *BaseStream) Episode() *anime.Episode {
	return s.episode
}

func (s *BaseStream) ListEntryData() *anime.EntryListData {
	return s.listEntryData
}

func (s *BaseStream) EpisodeCollection() *anime.EpisodeCollection {
	return s.episodeCollection
}

func (s *BaseStream) ClientId() string {
	return s.clientId
}

func (s *BaseStream) Terminate() {
	s.terminateOnce.Do(func() {
		// Cancel this stream's own context — does NOT affect other streams
		if s.streamCancelFunc != nil {
			s.streamCancelFunc()
		}

		// Cancel all active subtitle streams
		s.activeSubtitleStreams.Range(func(_ string, s *SubtitleStream) bool {
			s.cleanupFunc()
			return true
		})
		s.activeSubtitleStreams.Clear()

		s.subtitleEventCache.Clear()
	})
}

func (s *BaseStream) StreamError(err error) {
	s.logger.Error().Err(err).Msg("directstream: Stream error occurred")
	s.manager.nativePlayer.Error(s.clientId, err)
	s.Terminate()
	// Remove from the map without calling Terminate again
	s.manager.streams.Delete(s.clientId)
}

func (s *BaseStream) GetSubtitleEventCache() *result.Map[string, *mkvparser.SubtitleEvent] {
	return s.subtitleEventCache
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
// Helpers
////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// loadContentType loads the content type of the file.
// If the content type cannot be determined from the file extension,
// the first reader will be used to determine the content type.
func loadContentType(path string, reader ...io.ReadSeekCloser) string {
	ext := filepath.Ext(path)

	switch ext {
	case ".mp4":
		return "video/mp4"
	case ".mkv":
		//return "video/x-matroska"
		return "video/webm"
	case ".webm", ".m4v":
		return "video/webm"
	case ".avi":
		return "video/x-msvideo"
	case ".mov":
		return "video/quicktime"
	case ".flv":
		return "video/x-flv"
	default:
	}

	// No extension found
	// Read the first 1KB to determine the content type
	if len(reader) > 0 {
		if mimeType, ok := mkvparser.ReadIsMkvOrWebm(reader[0]); ok {
			return mimeType
		}
	}

	return ""
}

func (m *Manager) preStreamError(stream Stream, err error) {
	stream.Terminate()
	m.nativePlayer.Error(stream.ClientId(), err)
	m.streams.Delete(stream.ClientId())
}

func (m *Manager) getContentTypeAndLength(url string) (string, int64, error) {
	m.Logger.Trace().Msg("directstream(debrid): Fetching content type and length using HEAD request")

	// Create client with timeout for HEAD request (faster timeout since it's just headers)
	headClient := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Try HEAD request first
	resp, err := headClient.Head(url)
	if err == nil {
		defer resp.Body.Close()

		contentType := resp.Header.Get("Content-Type")
		contentLengthStr := resp.Header.Get("Content-Length")

		// Parse content length
		var length int64
		if contentLengthStr != "" {
			length, err = strconv.ParseInt(contentLengthStr, 10, 64)
			if err != nil {
				m.Logger.Error().Err(err).Str("contentType", contentType).Str("contentLength", contentLengthStr).
					Msg("directstream(debrid): Failed to parse content length from header")
				return "", 0, fmt.Errorf("failed to parse content length: %w", err)
			}
		}

		// If we have content type, return early
		if contentType != "" {
			return contentType, length, nil
		}

		m.Logger.Trace().Msg("directstream(debrid): Content type not found in HEAD response headers")
	} else {
		m.Logger.Trace().Err(err).Msg("directstream(debrid): HEAD request failed")
	}

	// Fall back to GET with Range request (either HEAD failed or no content type in headers)
	m.Logger.Trace().Msg("directstream(debrid): Falling back to GET request")

	// Create client with longer timeout for GET request (downloading content)
	getClient := &http.Client{
		Timeout: 15 * time.Second,
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create GET request: %w", err)
	}

	req.Header.Set("Range", "bytes=0-511")

	resp, err = getClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("GET request failed: %w", err)
	}
	defer resp.Body.Close()

	// Parse total length from Content-Range header (for Range requests)
	// Format: "bytes 0-511/1234567" where 1234567 is the total size
	var length int64
	if contentRange := resp.Header.Get("Content-Range"); contentRange != "" {
		// Extract total size from Content-Range header
		if idx := strings.LastIndex(contentRange, "/"); idx != -1 {
			totalSizeStr := contentRange[idx+1:]
			if totalSizeStr != "*" { // "*" means unknown size
				length, err = strconv.ParseInt(totalSizeStr, 10, 64)
				if err != nil {
					m.Logger.Warn().Err(err).Str("contentRange", contentRange).
						Msg("directstream(debrid): Failed to parse total size from Content-Range")
				}
			}
		}
	} else if contentLengthStr := resp.Header.Get("Content-Length"); contentLengthStr != "" {
		// Fallback to Content-Length if Content-Range not present (server might not support ranges)
		length, err = strconv.ParseInt(contentLengthStr, 10, 64)
		if err != nil {
			m.Logger.Warn().Err(err).Str("contentLength", contentLengthStr).
				Msg("directstream(debrid): Failed to parse content length from GET response")
		}
	}

	// Check if server provided Content-Type in GET response
	contentType := resp.Header.Get("Content-Type")
	if contentType != "" {
		return contentType, length, nil
	}

	// Read only what's needed for content type detection
	buf := make([]byte, 512)
	n, err := io.ReadFull(resp.Body, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", 0, fmt.Errorf("failed to read response body: %w", err)
	}

	contentType = http.DetectContentType(buf[:n])

	return contentType, length, nil
}

type StreamInfo struct {
	ContentType   string
	ContentLength int64
}

func (m *Manager) FetchStreamInfo(streamUrl string) (info *StreamInfo, canStream bool) {
	hasExtension, isArchive := IsArchive(streamUrl)

	m.Logger.Debug().Str("url", streamUrl).Msg("directstream(http): Fetching stream info")

	// If we were able to verify that the stream URL is an archive, we can't stream it
	if isArchive {
		m.Logger.Warn().Str("url", streamUrl).Msg("directstream(http): Stream URL is an archive, cannot stream")
		return nil, false
	}

	// If the stream URL has an extension, we can stream it
	if hasExtension {
		// Strip query params before checking extension
		cleanUrl := streamUrl
		if idx := strings.IndexByte(cleanUrl, '?'); idx != -1 {
			cleanUrl = cleanUrl[:idx]
		}
		ext := filepath.Ext(cleanUrl)
		// If not a valid video extension, we can't stream it
		if !util.IsValidVideoExtension(ext) {
			m.Logger.Warn().Str("url", streamUrl).Str("ext", ext).Msg("directstream(http): Stream URL has an invalid video extension, cannot stream")
			return nil, false
		}
	}

	// We'll fetch headers to get the info
	// If the headers are not available, we can't stream it

	contentType, contentLength, err := m.getContentTypeAndLength(streamUrl)
	if err != nil {
		m.Logger.Error().Err(err).Str("url", streamUrl).Msg("directstream(http): Failed to fetch content type and length")
		return nil, false
	}

	// If not a video content type, we can't stream it
	if !strings.HasPrefix(contentType, "video/") && contentType != "application/octet-stream" && contentType != "application/force-download" {
		m.Logger.Warn().Str("url", streamUrl).Str("contentType", contentType).Msg("directstream(http): Stream URL has an invalid content type, cannot stream")
		return nil, false
	}

	return &StreamInfo{
		ContentType:   contentType,
		ContentLength: contentLength,
	}, true
}

func IsArchive(streamUrl string) (hasExtension bool, isArchive bool) {
	// Strip query params before checking extension
	u := streamUrl
	if idx := strings.IndexByte(u, '?'); idx != -1 {
		u = u[:idx]
	}
	ext := filepath.Ext(u)
	if ext == ".zip" || ext == ".rar" {
		return true, true
	}

	if ext != "" {
		return true, false
	}

	return false, false
}
