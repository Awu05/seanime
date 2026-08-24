package torrentstream

import (
	"context"
	"errors"
	"fmt"
	"seanime/internal/api/anilist"
	"seanime/internal/api/metadata"
	"seanime/internal/directstream"
	"seanime/internal/events"
	hibiketorrent "seanime/internal/extension/hibike/torrent"
	"seanime/internal/hook"
	"seanime/internal/library/playbackmanager"
	"seanime/internal/util"
	"seanime/internal/videocore"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/samber/mo"
)

// errStreamSuperseded is returned by waitUntilReadyToStream when a newer StartStream call has
// taken over. It's not a real failure - callers should abort quietly rather than surface an error.
var errStreamSuperseded = errors.New("torrentstream: stream superseded by a newer StartStream call")

// waitReadyPollInterval / waitReadyStallDeadline are vars (not consts) so tests can shrink them
// instead of waiting out the real durations.
var (
	waitReadyPollInterval  = 3 * time.Second
	waitReadyStallDeadline = 45 * time.Second
)

// waitUntilReadyToStream blocks until the client's current torrent/file becomes ready to
// stream. It gives up and returns an error if: a newer StartStream call has superseded this one
// (errStreamSuperseded); the torrent/client gets dropped; or the download makes no progress for
// waitReadyStallDeadline (instead of leaving the caller polling forever with no way to tell a
// legitimately slow torrent from a stalled/seederless one).
func (r *Repository) waitUntilReadyToStream(generation int64) error {
	var lastBytes int64 = -1
	lastProgressAt := time.Now()

	for {
		if r.startStreamGeneration.Load() != generation {
			return errStreamSuperseded
		}
		torrentOpt, fileOpt := r.client.currentTorrentAndFile()
		if r.client.torrentClient.IsAbsent() || torrentOpt.IsAbsent() {
			return errors.New("torrentstream: torrent was dropped before it became ready to stream")
		}
		if r.client.readyToStream() {
			return nil
		}

		if file, ok := fileOpt.Get(); ok {
			bytes := file.BytesCompleted()
			if bytes > lastBytes {
				lastBytes = bytes
				lastProgressAt = time.Now()
			} else if time.Since(lastProgressAt) > waitReadyStallDeadline {
				return fmt.Errorf("torrentstream: download stalled, no progress in %s", waitReadyStallDeadline)
			}
		}

		r.logger.Debug().Msg("torrentstream: Waiting for playable threshold to be reached")
		time.Sleep(waitReadyPollInterval)
	}
}

type PlaybackType string

const (
	PlaybackTypeExternal           PlaybackType = "default" // External player
	PlaybackTypeExternalPlayerLink PlaybackType = "externalPlayerLink"
	PlaybackTypeNativePlayer       PlaybackType = "nativeplayer"
	PlaybackTypeNone               PlaybackType = "none"
	PlaybackTypeNoneAndAwait       PlaybackType = "noneAndAwait"
)

type StartStreamOptions struct {
	MediaId           int
	EpisodeNumber     int                         // RELATIVE Episode number to identify the file
	AniDBEpisode      string                      // Animap episode
	AutoSelect        bool                        // Automatically select the best file to stream
	Torrent           *hibiketorrent.AnimeTorrent // Selected torrent (Manual selection)
	FileIndex         *int                        // Index of the file to stream (Manual selection)
	UserAgent         string
	ClientId          string
	PlaybackType      PlaybackType
	BatchEpisodeFiles *hibiketorrent.BatchEpisodeFiles
}

// StartStream is called by the client to start streaming a torrent
func (r *Repository) StartStream(ctx context.Context, opts *StartStreamOptions) (err error) {
	defer util.HandlePanicInModuleWithError("torrentstream/stream/StartStream", &err)
	// DEVNOTE: Do not
	//r.Shutdown()

	r.previousStreamOptions = mo.Some(opts)

	// Bumped so the readiness-polling goroutines below abort once a newer StartStream call
	// (e.g. rapid episode switching) supersedes this one, instead of running to completion
	// against whatever torrent/file happens to be "current" by the time they finish waiting.
	myGeneration := r.startStreamGeneration.Add(1)

	r.logger.Info().
		Str("clientId", opts.ClientId).
		Any("playbackType", opts.PlaybackType).
		Int("mediaId", opts.MediaId).Msgf("torrentstream: Starting stream for episode %s", opts.AniDBEpisode)

	r.sendStateEvent(eventLoading)
	r.wsEventManager.SendEvent(events.ShowIndefiniteLoader, "torrentstream")
	defer func() {
		r.wsEventManager.SendEvent(events.HideIndefiniteLoader, "torrentstream")
	}()

	if opts.PlaybackType == PlaybackTypeNativePlayer {
		r.directStreamManager.PrepareNewStream(opts.ClientId, "Selecting torrent...")
	}

	//
	// Get the media info
	//
	media, _, err := r.GetMediaInfo(ctx, opts.MediaId)
	if err != nil {
		return err
	}

	episodeNumber := opts.EpisodeNumber
	aniDbEpisode := opts.AniDBEpisode
	//
	// Check if there's a prepared stream that matches this request
	//
	var torrentToStream *playbackTorrent
	usedPreparedStream := false

	if prepared, ok := r.takePreloadedStream(); ok {
		if streamOptionsMatch(opts, prepared.Options) {
			r.logger.Info().Msg("torrentstream: Using pre-downloaded stream")
			torrentToStream = &playbackTorrent{
				Torrent: prepared.Torrent,
				File:    prepared.File,
			}
			usedPreparedStream = true

			// Cancel the prepared stream context - it's now the active stream, not just prepared
			if prepared.CancelFunc != nil {
				prepared.CancelFunc()
			}
		} else {
			// Different episode requested, cancel and drop the prepared stream
			r.logger.Debug().Msg("torrentstream: Prepared stream doesn't match request, cancelling it")
			r.cancelAndMaybeDropPreloaded(prepared)
		}
	}

	//
	// Find the best torrent / Select the torrent (only if not using prepared stream)
	//
	if !usedPreparedStream {
		if opts.AutoSelect {
			torrentToStream, err = r.findBestTorrent(media, aniDbEpisode, episodeNumber)
			if err != nil {
				if opts.PlaybackType == PlaybackTypeNativePlayer {
					r.directStreamManager.AbortOpen(opts.ClientId, err)
				}
				r.sendStateEvent(eventLoadingFailed)
				return err
			}
		} else {
			if opts.Torrent == nil {
				return fmt.Errorf("torrentstream: No torrent provided")
			}
			torrentToStream, err = r.findBestTorrentFromManualSelection(opts.Torrent, media, aniDbEpisode, opts.FileIndex)
			if err != nil {
				if opts.PlaybackType == PlaybackTypeNativePlayer {
					r.directStreamManager.AbortOpen(opts.ClientId, err)
				}
				r.sendStateEvent(eventLoadingFailed)
				return err
			}
		}
	}

	if torrentToStream == nil {
		if opts.PlaybackType == PlaybackTypeNativePlayer {
			r.directStreamManager.AbortOpen(opts.ClientId, fmt.Errorf("torrentstream: No torrent found"))
		}
		r.sendStateEvent(eventLoadingFailed)
		return fmt.Errorf("torrentstream: No torrent selected")
	}

	//
	// Set current file & torrent
	//
	r.currentClientIdMu.Lock()
	r.currentClientId = opts.ClientId
	r.currentClientIdMu.Unlock()
	// Legacy fields are guarded by c.mu — the monitor loop reads them under the same lock.
	r.client.mu.Lock()
	r.client.currentFile = mo.Some(torrentToStream.File)
	r.client.currentTorrent = mo.Some(torrentToStream.Torrent)
	r.client.mu.Unlock()
	r.client.SetActiveStream(opts.ClientId, torrentToStream.Torrent, torrentToStream.File)

	r.sendStateEvent(eventLoading, TLSStateSendingStreamToMediaPlayer)

	go func() {
		// Add the torrent to the history if it is a batch & manually selected
		if len(r.client.currentTorrent.MustGet().Files()) > 1 && opts.Torrent != nil && opts.Torrent.IsBatch {
			r.AddBatchHistory(opts.MediaId, opts.Torrent, opts.BatchEpisodeFiles) // ran in goroutine
		}
	}()

	//
	// Start the playback
	//
	go func() {
		switch opts.PlaybackType {
		case PlaybackTypeNone:
			r.logger.Warn().Msg("torrentstream: Playback type is set to 'none'")
			// Signal to the client that the torrent has started playing (remove loading status)
			// There will be no tracking
			r.sendStateEvent(eventTorrentStartedPlaying)
		case PlaybackTypeNoneAndAwait:
			r.logger.Warn().Msg("torrentstream: Playback type is set to 'noneAndAwait'")
			// Signal to the client that the torrent has started playing (remove loading status)
			// There will be no tracking
			if err := r.waitUntilReadyToStream(myGeneration); err != nil {
				if !errors.Is(err, errStreamSuperseded) {
					r.logger.Warn().Err(err).Msg("torrentstream: Stream never became ready")
					r.sendStateEvent(eventLoadingFailed)
				}
				return
			}
			r.sendStateEvent(eventTorrentStartedPlaying)
		//
		// External player
		//
		case PlaybackTypeExternal, PlaybackTypeExternalPlayerLink:
			r.sendStreamToExternalPlayer(myGeneration, opts, media, aniDbEpisode)
		//
		// Direct stream
		//
		case PlaybackTypeNativePlayer:
			readyCh, err := r.directStreamManager.PlayTorrentStream(ctx, directstream.PlayTorrentStreamOptions{
				ClientId:      opts.ClientId,
				EpisodeNumber: opts.EpisodeNumber,
				AnidbEpisode:  opts.AniDBEpisode,
				Media:         media.ToBaseAnime(),
				Torrent:       r.client.currentTorrent.MustGet(),
				File:          r.client.currentFile.MustGet(),
				OnTerminate: func() {
					_ = r.StopStream(true)
				},
			})
			if err != nil {
				r.logger.Error().Err(err).Msg("torrentstream: Failed to prepare new stream")
				r.sendStateEvent(eventLoadingFailed)
				return
			}

			if opts.PlaybackType == PlaybackTypeNativePlayer {
				r.directStreamManager.PrepareNewStream(opts.ClientId, "Downloading metadata...")
			}

			// Make sure the client is ready and the torrent is partially downloaded
			if err := r.waitUntilReadyToStream(myGeneration); err != nil {
				if !errors.Is(err, errStreamSuperseded) {
					r.logger.Warn().Err(err).Msg("torrentstream: Stream never became ready")
					r.directStreamManager.AbortOpen(opts.ClientId, err)
					r.sendStateEvent(eventLoadingFailed)
				}
				return
			}
			close(readyCh)
		}
	}()

	r.sendStateEvent(eventTorrentLoaded)
	r.logger.Info().Msg("torrentstream: Stream started")

	return nil
}

// sendStreamToExternalPlayer sends the stream to the desktop player or external player link.
// It blocks until the some pieces have been downloaded before sending the stream for faster playback.
func (r *Repository) sendStreamToExternalPlayer(generation int64, opts *StartStreamOptions, completeAnime *anilist.CompleteAnime, aniDbEpisode string) {

	baseAnime := completeAnime.ToBaseAnime()

	r.wsEventManager.SendEvent(events.ShowIndefiniteLoader, "torrentstream")
	defer func() {
		r.wsEventManager.SendEvent(events.HideIndefiniteLoader, "torrentstream")
	}()

	// Make sure the client is ready and the torrent is partially downloaded
	if err := r.waitUntilReadyToStream(generation); err != nil {
		if !errors.Is(err, errStreamSuperseded) {
			r.logger.Warn().Err(err).Msg("torrentstream: Stream never became ready")
			r.sendStateEvent(eventLoadingFailed)
		}
		return
	}

	event := &TorrentStreamSendStreamToMediaPlayerEvent{
		WindowTitle:  "",
		StreamURL:    r.client.GetStreamingUrl(opts.ClientId),
		Media:        baseAnime,
		AniDbEpisode: aniDbEpisode,
		PlaybackType: string(opts.PlaybackType),
	}
	err := hook.GlobalHookManager.OnTorrentStreamSendStreamToMediaPlayer().Trigger(event)
	if err != nil {
		r.logger.Error().Err(err).Msg("torrentstream: Failed to trigger hook")
		return
	}
	windowTitle := event.WindowTitle
	streamURL := event.StreamURL
	baseAnime = event.Media
	aniDbEpisode = event.AniDbEpisode
	playbackType := PlaybackType(event.PlaybackType)

	if event.DefaultPrevented {
		r.logger.Debug().Msg("torrentstream: Stream prevented by hook")
		return
	}

	switch playbackType {
	//
	// Desktop player
	//
	case PlaybackTypeExternal:
		r.logger.Debug().Msgf("torrentstream: Starting the media player %s", streamURL)
		err = r.playbackManager.StartStreamingUsingMediaPlayer(windowTitle, &playbackmanager.StartPlayingOptions{
			Payload:   streamURL,
			UserAgent: opts.UserAgent,
			ClientId:  opts.ClientId,
		}, baseAnime, aniDbEpisode)
		if err != nil {
			// Failed to start the stream, we'll drop the torrents and stop the server
			r.sendStateEvent(eventLoadingFailed)
			_ = r.StopStream()
			r.logger.Error().Err(err).Msg("torrentstream: Failed to start the stream")
			r.wsEventManager.SendEventTo(opts.ClientId, events.ErrorToast, err.Error())
		}

		r.wsEventManager.SendEvent(events.ShowIndefiniteLoader, "torrentstream")
		defer func() {
			r.wsEventManager.SendEvent(events.HideIndefiniteLoader, "torrentstream")
		}()

		r.playbackManager.RegisterMediaPlayerCallback(func(event playbackmanager.PlaybackEvent) bool {
			switch event.(type) {
			case playbackmanager.StreamStartedEvent:
				r.logger.Debug().Msg("torrentstream: Media player started playing")
				r.wsEventManager.SendEvent(events.HideIndefiniteLoader, "torrentstream")
				return false
			}
			return true
		})

	//
	// External player link
	//
	case PlaybackTypeExternalPlayerLink:
		r.logger.Debug().Msgf("torrentstream: Sending stream to external player %s", streamURL)
		r.wsEventManager.SendEventTo(opts.ClientId, events.ExternalPlayerOpenURL, struct {
			Url           string `json:"url"`
			MediaId       int    `json:"mediaId"`
			EpisodeNumber int    `json:"episodeNumber"`
			MediaTitle    string `json:"mediaTitle"`
		}{
			Url:           r.client.GetExternalPlayerStreamingUrl(opts.ClientId),
			MediaId:       opts.MediaId,
			EpisodeNumber: opts.EpisodeNumber,
			MediaTitle:    baseAnime.GetPreferredTitle(),
		})

		// Signal to the client that the torrent has started playing (remove loading status)
		// We can't know for sure
		r.sendStateEvent(eventTorrentStartedPlaying)
	}
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type StartUntrackedStreamOptions struct {
	Magnet       string
	FileIndex    int
	WindowTitle  string
	UserAgent    string
	ClientId     string
	PlaybackType PlaybackType
}

// StopStream stops the stream and closes the server.
// If fromNativePlayer is true, it will not stop the native player again.
func (r *Repository) StopStream(fromNativePlayer ...bool) error {
	defer func() {
		if rec := recover(); rec != nil {
			logRecoveredPanic(r.logger, "StopStream", rec)
		}
	}()
	r.logger.Info().Msg("torrentstream: Stopping stream")

	// Stop the client
	// This will stop the stream and close the server
	// This also sends the eventTorrentStopped event
	r.client.mu.Lock()
	r.client.repository.logger.Debug().Msg("torrentstream: Stopping stream and freeing this session's resources")

	hadTorrent := r.client.currentTorrent.IsPresent()

	// Release this session's claims FIRST so the drop below only sees
	// claims still held by other sessions (and other tabs of this one).
	r.currentClientIdMu.RLock()
	clientId := r.currentClientId
	r.currentClientIdMu.RUnlock()
	if clientId != "" {
		r.client.RemoveActiveStream(clientId)
	}

	// Reset all torrent state
	r.client.currentTorrent = mo.None[*torrent.Torrent]()
	r.client.currentFile = mo.None[*torrent.File]()
	r.client.currentTorrentStatus = TorrentStatus{}

	// Clear preloaded/prepared stream
	if ps, ok := r.takePreloadedStream(); ok && ps.CancelFunc != nil {
		ps.CancelFunc()
	}

	// Drop torrents no session claims any more — stops seeding and frees disk
	// without touching torrents other users are still streaming
	if hadTorrent {
		r.client.dropUnclaimedTorrents()
	}

	// Reset playback state
	r.playback.currentVideoDuration = 0
	if r.playback.mediaPlayerCtxCancelFunc != nil {
		r.playback.mediaPlayerCtxCancelFunc()
		r.playback.mediaPlayerCtxCancelFunc = nil
	}

	// Send stopped event and stop media player
	r.client.repository.sendStateEvent(eventTorrentStopped, nil)
	r.client.repository.mediaPlayerRepository.Stop()
	r.client.mu.Unlock()

	if len(fromNativePlayer) == 0 || fromNativePlayer[0] == false {
		go func() {
			if playbackType, ok := r.nativePlayer.VideoCore().GetCurrentPlaybackType(); ok && playbackType == videocore.PlaybackTypeTorrent {
				r.nativePlayer.Stop()
			}
		}()
	}

	r.logger.Info().Msg("torrentstream: Stream stopped, all resources freed")

	return nil
}

func (r *Repository) DropTorrent() error {
	r.logger.Info().Msg("torrentstream: Dropping unclaimed torrents")

	if r.client.torrentClient.IsAbsent() {
		return nil
	}

	r.client.dropUnclaimedTorrents()

	r.mediaPlayerRepository.Stop()

	r.logger.Info().Msg("torrentstream: Dropped last torrent")

	return nil
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

func (r *Repository) GetMediaInfo(ctx context.Context, mediaId int) (media *anilist.CompleteAnime, animeMetadata *metadata.AnimeMetadata, err error) {
	// Get the media
	var found bool
	media, found = r.completeAnimeCache.Get(mediaId)
	if !found {
		// Fetch the media
		media, err = r.platformRef.Get().GetAnimeWithRelations(ctx, mediaId)
		if err != nil {
			baseAnime, lErr := r.platformRef.Get().GetAnime(ctx, mediaId)
			if lErr != nil {
				return nil, nil, fmt.Errorf("torrentstream: Failed to fetch media: %w", err)
			}
			media = baseAnime.ToCompleteAnime()
			err = nil
		}
	}

	// Get the media
	animeMetadata, err = r.metadataProviderRef.Get().GetAnimeMetadata(metadata.AnilistPlatform, mediaId)
	if err != nil {
		//return nil, nil, fmt.Errorf("torrentstream: Could not fetch AniDB media: %w", err)
		animeMetadata = &metadata.AnimeMetadata{
			Titles:       make(map[string]string),
			Episodes:     make(map[string]*metadata.EpisodeMetadata),
			EpisodeCount: 0,
			SpecialCount: 0,
			Mappings: &metadata.AnimeMappings{
				AnilistId: media.GetID(),
			},
		}
		animeMetadata.Titles["en"] = media.GetTitleSafe()
		animeMetadata.Titles["x-jat"] = media.GetRomajiTitleSafe()
		err = nil
	}

	return
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
// PreloadStream
//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// streamOptionsMatch checks if two stream options represent the same episode
func streamOptionsMatch(a, b *StartStreamOptions) bool {
	if a == nil || b == nil {
		return false
	}
	return a.MediaId == b.MediaId && a.EpisodeNumber == b.EpisodeNumber
}

// PreloadStream starts pre-downloading a stream at reduced speed to avoid interfering with current playback
func (r *Repository) PreloadStream(ctx context.Context, opts *StartStreamOptions) (err error) {
	defer util.HandlePanicInModuleWithError("torrentstream/stream/PreloadStream", &err)

	r.logger.Info().
		Int("mediaId", opts.MediaId).
		Int("episodeNumber", opts.EpisodeNumber).
		Msg("torrentstream: Preloading stream for future playback")

	// Cancel any existing prepared stream
	if prepared, ok := r.takePreloadedStream(); ok {
		r.logger.Debug().Msg("torrentstream: Cancelling existing preloaded stream")
		if prepared.CancelFunc != nil {
			prepared.CancelFunc()
		}
	}

	// Get media info
	media, _, err := r.GetMediaInfo(ctx, opts.MediaId)
	if err != nil {
		return err
	}

	// Find best torrent
	var torrentToStream *playbackTorrent
	if opts.AutoSelect {
		torrentToStream, err = r.findBestTorrent(media, opts.AniDBEpisode, opts.EpisodeNumber)
		if err != nil {
			r.logger.Error().Err(err).Msg("torrentstream: Failed to find torrent for preloading")
			return err
		}
	} else {
		if opts.Torrent == nil {
			return fmt.Errorf("torrentstream: No torrent provided")
		}
		torrentToStream, err = r.findBestTorrentFromManualSelection(opts.Torrent, media, opts.AniDBEpisode, opts.FileIndex)
		if err != nil {
			r.logger.Error().Err(err).Msg("torrentstream: Failed to select torrent for preloading")
			return err
		}
	}

	if torrentToStream == nil {
		return fmt.Errorf("torrentstream: No torrent selected for preloading")
	}

	// Create a cancellable context for this prepared stream
	prepareCtx, cancelFunc := context.WithCancel(ctx)

	r.logger.Info().
		Str("torrent", torrentToStream.Torrent.Name()).
		Msg("torrentstream: Started preloading stream")

	// Store prepared stream info
	r.setPreloadedStream(&preloadedStream{
		Torrent:    torrentToStream.Torrent,
		File:       torrentToStream.File,
		Options:    opts,
		CancelFunc: cancelFunc,
	})

	// Start downloading in background
	go func() {
		<-prepareCtx.Done()
		r.logger.Debug().Msg("torrentstream: Prepared stream context cancelled")
	}()

	return nil
}

// CancelPreparedStream cancels any ongoing stream preloading
func (r *Repository) CancelPreparedStream() {
	if prepared, ok := r.takePreloadedStream(); ok {
		r.logger.Debug().Msg("torrentstream: Cancelling prepared stream")
		r.cancelAndMaybeDropPreloaded(prepared)
	}
}
