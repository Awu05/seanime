package torrentstream

import (
	"context"
	"seanime/internal/mediaplayers/mediaplayer"
	"seanime/internal/videocore"
	"sync"
	"sync/atomic"
)

type (
	playback struct {
		// listenerMu guards the desktop media player fields, which settings changes replace.
		listenerMu               sync.Mutex
		mediaPlayerCtxCancelFunc context.CancelFunc
		// desktopPlayerStream is set while this repository's stream plays in the desktop media
		// player. Every profile shares that player, so only then are its events this one's.
		desktopPlayerStream atomic.Bool
		// currentVideoDuration is the playing video's duration, or 0 until the player reports
		// one; the first swap away from 0 is what sends eventTorrentStartedPlaying.
		currentVideoDuration atomic.Int64
	}
)

func (r *Repository) mediaPlayer() *mediaplayer.Repository {
	r.playback.listenerMu.Lock()
	defer r.playback.listenerMu.Unlock()
	return r.mediaPlayerRepository
}

// listenToMediaPlayerEventsLocked (re)subscribes to the desktop media player for the lifetime of
// this repository. The caller holds listenerMu.
func (r *Repository) listenToMediaPlayerEventsLocked() {
	if r.playback.mediaPlayerCtxCancelFunc != nil {
		r.playback.mediaPlayerCtxCancelFunc()
	}
	var ctx context.Context
	ctx, r.playback.mediaPlayerCtxCancelFunc = context.WithCancel(context.Background())
	sub := r.mediaPlayerRepository.Subscribe(r.mediaPlayerSubscriberID)

	go func() {
		for {
			select {
			case <-ctx.Done():
				r.logger.Debug().Msg("torrentstream: Media player context cancelled")
				return
			case event := <-sub.EventCh:
				if !r.playback.desktopPlayerStream.Load() {
					continue
				}
				switch e := event.(type) {
				case mediaplayer.StreamingTrackingStartedEvent:
					r.playback.currentVideoDuration.Store(0)
					if settings, ok := r.getSettings(); ok {
						r.shouldPreloadStream.Store(settings.PreloadNextStream)
					}
				case mediaplayer.StreamingTrackingStoppedEvent:
					go func() {
						defer func() {
							if rec := recover(); rec != nil {
								logRecoveredPanic(r.logger, "StreamingTrackingStoppedEvent handler", rec)
							}
						}()
						r.logger.Debug().Msg("torrentstream: Media player stopped event received")
						_ = r.StopStream()
					}()
				case mediaplayer.StreamingPlaybackStatusEvent:
					if e.Status == nil {
						continue
					}
					select {
					case r.client.mediaPlayerPlaybackStatusCh <- e.Status:
					default: // monitorLoop hasn't consumed the previous status yet
					}
					if r.shouldPreloadStream.Load() && e.Status.CompletionPercentage >= 0.5 {
						r.shouldPreloadStream.Store(false)
						r.sendStateEvent(eventPreloadNextStream)
					}
				}
			}
		}
	}()
}

func (r *Repository) stopMediaPlayerListener() {
	r.playback.listenerMu.Lock()
	defer r.playback.listenerMu.Unlock()
	if r.playback.mediaPlayerCtxCancelFunc != nil {
		r.playback.mediaPlayerCtxCancelFunc()
		r.playback.mediaPlayerCtxCancelFunc = nil
	}
	if r.mediaPlayerRepository != nil {
		r.mediaPlayerRepository.Unsubscribe(r.mediaPlayerSubscriberID)
	}
}

// ListenToNativePlayerEvents reacts to the native player this repository streams through. Each
// profile session has its own player, so each session's repository must call this for itself.
func (r *Repository) ListenToNativePlayerEvents() {
	r.nativePlayer.VideoCore().Unsubscribe("torrentstream")
	r.logger.Trace().Msg("torrentstream: Subscribing to video core events")
	videoCoreSubscriber := r.nativePlayer.VideoCore().Subscribe("torrentstream")

	go func(sub *videocore.Subscriber) {
		defer func() {
			r.logger.Trace().Msg("torrentstream: Stopping video core listener")
		}()
		for e := range sub.Events() {
			// get the player type from the event instead of the instance
			if e.GetPlayerType() != videocore.NativePlayer {
				continue
			}

			switch event := e.(type) {
			case *videocore.VideoLoadedEvent:
				r.logger.Debug().Msg("torrentstream: Native player loaded event received")
				r.playback.currentVideoDuration.Store(0)
				if settings, ok := r.getSettings(); ok {
					r.shouldPreloadStream.Store(settings.PreloadNextStream)
				}
			case *videocore.VideoLoadedMetadataEvent:
				_, fileOpt := r.client.currentTorrentAndFile()
				if fileOpt.IsPresent() && event.Duration > 0 && r.playback.currentVideoDuration.CompareAndSwap(0, int64(event.Duration)) {
					r.logger.Debug().Msg("torrentstream: Media player started playing the video, sending event")
					r.sendStateEvent(eventTorrentStartedPlaying)
				}
			case *videocore.VideoStatusEvent:
				if event.Duration > 0 && event.CurrentTime/event.Duration >= 0.5 && r.shouldPreloadStream.Load() {
					r.shouldPreloadStream.Store(false)
					r.sendStateEvent(eventPreloadNextStream)
				}
			case *videocore.VideoTerminatedEvent:
				r.logger.Debug().Msg("torrentstream: Native player terminated event received")
				r.playback.currentVideoDuration.Store(0)
				// Only handle the event if we actually have a current torrent to avoid unnecessary cleanup
				if torrentOpt, _ := r.client.currentTorrentAndFile(); torrentOpt.IsPresent() {
					go func() {
						defer func() {
							if rec := recover(); rec != nil {
								logRecoveredPanic(r.logger, "VideoTerminatedEvent handler", rec)
							}
						}()
						r.logger.Debug().Msg("torrentstream: Stopping stream due to native player termination")
						// Stop the stream
						_ = r.StopStream()
					}()
				}
			}

		}
	}(videoCoreSubscriber)
}
