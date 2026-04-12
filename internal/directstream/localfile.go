package directstream

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"seanime/internal/api/anilist"
	"seanime/internal/library/anime"
	"seanime/internal/mkvparser"
	"seanime/internal/nativeplayer"
	"seanime/internal/util"
	"seanime/internal/util/result"
	"time"

	"github.com/google/uuid"
	"github.com/samber/mo"
)

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
// Local File
////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

var _ Stream = (*LocalFileStream)(nil)

// LocalFileStream is a stream that is a local file.
type LocalFileStream struct {
	BaseStream
	localFile *anime.LocalFile
}

func (s *LocalFileStream) newReader() (io.ReadSeekCloser, error) {
	r, err := os.OpenFile(s.localFile.Path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}

	return r, nil
}

func (s *LocalFileStream) Type() nativeplayer.StreamType {
	return nativeplayer.StreamTypeFile
}

func (s *LocalFileStream) LoadContentType() string {
	s.contentTypeOnce.Do(func() {
		// No need to pass a reader because we are not going to read the file
		// Get the mime type from the file extension
		s.contentType = loadContentType(s.localFile.Path)
	})

	return s.contentType
}

func (s *LocalFileStream) LoadPlaybackInfo() (ret *nativeplayer.PlaybackInfo, err error) {
	s.playbackInfoOnce.Do(func() {
		if s.localFile == nil {
			s.playbackInfo = &nativeplayer.PlaybackInfo{}
			err = fmt.Errorf("local file is not set")
			s.playbackInfoErr = err
			return
		}

		// Open the file
		fr, err := s.newReader()
		if err != nil {
			s.logger.Error().Err(err).Msg("directstream(file): Failed to open local file")
			s.playbackInfoErr = fmt.Errorf("cannot open local file: %w", err)
			return
		}

		// Close the file when done
		defer func() {
			if closer, ok := fr.(io.Closer); ok {
				s.logger.Trace().Msg("directstream(file): Closing local file reader")
				_ = closer.Close()
			} else {
				s.logger.Trace().Msg("directstream(file): Local file reader does not implement io.Closer")
			}
		}()

		// Get the file size
		size, err := fr.Seek(0, io.SeekEnd)
		if err != nil {
			s.logger.Error().Err(err).Msg("directstream(file): Failed to get file size")
			s.playbackInfoErr = fmt.Errorf("failed to get file size: %w", err)
			return
		}
		_, _ = fr.Seek(0, io.SeekStart)

		id := uuid.New().String()

		var entryListData *anime.EntryListData
		if animeCollection := s.manager.animeCollection.Load(); animeCollection != nil {
			if listEntry, ok := animeCollection.GetListEntryFromAnimeId(s.media.ID); ok {
				entryListData = anime.NewEntryListData(listEntry)
			}
		}

		playbackInfo := nativeplayer.PlaybackInfo{
			ID:                id,
			StreamType:        s.Type(),
			StreamPath:        s.localFile.Path,
			MimeType:          s.LoadContentType(),
			StreamUrl:         "{{SERVER_URL}}/api/v1/directstream/stream?id=" + id + "&clientId=" + s.clientId + s.manager.GetHMACTokenQueryParam("/api/v1/directstream/stream", "&"),
			ContentLength:     size,
			MkvMetadata:       nil,
			MkvMetadataParser: mo.None[*mkvparser.MetadataParser](),
			Episode:           s.episode,
			Media:             s.media,
			EntryListData:     entryListData,
			LocalFile:         s.localFile,
		}

		// If the content type is an EBML content type, we can create a metadata parser
		if isEbmlContent(s.LoadContentType()) {

			parserKey := util.Base64EncodeStr(s.localFile.Path)

			parser, ok := s.manager.parserCache.Get(parserKey)
			if !ok {
				parser = mkvparser.NewMetadataParser(fr, s.logger)
				s.manager.parserCache.SetT(parserKey, parser, 2*time.Hour)
			}

			metadata := parser.GetMetadata(context.Background())
			if metadata.Error != nil {
				s.logger.Error().Err(metadata.Error).Msg("directstream(torrent): Failed to get metadata")
				s.playbackInfoErr = fmt.Errorf("failed to get metadata: %w", metadata.Error)
				return
			}

			playbackInfo.MkvMetadata = metadata
			playbackInfo.MkvMetadataParser = mo.Some(parser)
		}

		s.playbackInfo = &playbackInfo
	})

	return s.playbackInfo, s.playbackInfoErr
}

func (s *LocalFileStream) GetAttachmentByName(filename string) (*mkvparser.AttachmentInfo, bool) {
	return getAttachmentByName(s.streamCtx, s, filename)
}

func (s *LocalFileStream) GetStreamHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			// Get the file size
			fileInfo, err := os.Stat(s.localFile.Path)
			if err != nil {
				s.logger.Error().Msg("directstream: Failed to get file info")
				http.Error(w, "Failed to get file info", http.StatusInternalServerError)
				return
			}

			// Set the content length
			w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))
			w.Header().Set("Content-Type", s.LoadContentType())
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", s.localFile.Path))
			w.WriteHeader(http.StatusOK)
		} else {
			ServeLocalFile(w, r, s)
		}
	})
}

func ServeLocalFile(w http.ResponseWriter, r *http.Request, lfStream *LocalFileStream) {
	playbackInfo, err := lfStream.LoadPlaybackInfo()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	size := playbackInfo.ContentLength

	if isThumbnailRequest(r) {
		reader, err := lfStream.newReader()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		ra, ok := handleRange(w, r, reader, lfStream.localFile.Path, size)
		if !ok {
			return
		}
		serveContentRange(w, r, r.Context(), reader, lfStream.localFile.Path, size, playbackInfo.MimeType, ra)
		return
	}

	if lfStream.serveContentCancelFunc != nil {
		lfStream.serveContentCancelFunc()
	}

	ct, cancel := context.WithCancel(lfStream.streamCtx)
	lfStream.serveContentCancelFunc = cancel

	reader, err := lfStream.newReader()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer reader.Close()

	ra, ok := handleRange(w, r, reader, lfStream.localFile.Path, size)
	if !ok {
		return
	}

	if _, ok := playbackInfo.MkvMetadataParser.Get(); ok {
		// Start a subtitle stream from the current position
		subReader, err := lfStream.newReader()
		if err != nil {
			lfStream.logger.Error().Err(err).Msg("directstream: Failed to create subtitle reader")
			http.Error(w, "Failed to create subtitle reader", http.StatusInternalServerError)
			return
		}
		go lfStream.StartSubtitleStream(lfStream, lfStream.streamCtx, subReader, ra.Start)
	}

	serveContentRange(w, r, ct, reader, lfStream.localFile.Path, size, playbackInfo.MimeType, ra)
}

// PlayLocalFileDirect plays an arbitrary local file path through the native player
// without anime metadata. Used by the debrid torrent list "play locally" button to
// play a file that was downloaded locally but isn't part of the scanned anime library.
func (m *Manager) PlayLocalFileDirect(clientId string, path string, title string) error {
	m.playbackMu.Lock()
	defer m.playbackMu.Unlock()

	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("local file not found: %w", err)
	}

	// Stub metadata — this is a "play arbitrary file" path with no media/episode match.
	stubMedia := &anilist.BaseAnime{ID: 0}
	stubEpisode := &anime.Episode{
		Type:           anime.LocalFileTypeMain,
		DisplayTitle:   title,
		EpisodeTitle:   "",
		EpisodeNumber:  1,
		ProgressNumber: 1,
		AniDBEpisode:   "1",
	}
	stubCollection := &anime.EpisodeCollection{
		Episodes: []*anime.Episode{stubEpisode},
	}
	stubLocalFile := &anime.LocalFile{
		Path: path,
		Name: filepath.Base(path),
	}

	stream := &LocalFileStream{
		localFile: stubLocalFile,
		BaseStream: BaseStream{
			manager:               m,
			logger:                m.Logger,
			clientId:              clientId,
			filename:              filepath.Base(path),
			media:                 stubMedia,
			episode:               stubEpisode,
			episodeCollection:     stubCollection,
			subtitleEventCache:    result.NewMap[string, *mkvparser.SubtitleEvent](),
			activeSubtitleStreams: result.NewMap[string, *SubtitleStream](),
		},
	}

	go func() {
		m.loadStream(stream)
	}()

	return nil
}

type PlayLocalFileOptions struct {
	ClientId   string
	Path       string
	LocalFiles []*anime.LocalFile
}

// PlayLocalFile is used by a module to load a new torrent stream.
func (m *Manager) PlayLocalFile(ctx context.Context, opts PlayLocalFileOptions) error {
	m.playbackMu.Lock()
	defer m.playbackMu.Unlock()

	// Get the local file
	var lf *anime.LocalFile
	for _, l := range opts.LocalFiles {
		if util.NormalizePath(l.Path) == util.NormalizePath(opts.Path) {
			lf = l
			break
		}
	}

	if lf == nil {
		return fmt.Errorf("cannot play local file, could not find local file: %s", opts.Path)
	}

	if lf.MediaId == 0 {
		return fmt.Errorf("local file has not been matched to a media: %s", opts.Path)
	}

	mId := lf.MediaId

	// Try to load the anime collection. If it's nil (background seed failed or is still
	// in-flight), lazy-load from the platform cache. If that also fails, fall back to
	// an empty collection so local file playback still works — media metadata is fetched
	// via getAnime() which has its own platform fallback.
	animeCollection := m.animeCollection.Load()
	if animeCollection == nil {
		if collection, err := m.platformRef.Get().GetAnimeCollection(ctx, false); err == nil && collection != nil {
			m.animeCollection.Store(collection)
			animeCollection = collection
		} else {
			m.Logger.Warn().Err(err).Msg("directstream: Falling back to empty anime collection for local file playback")
			animeCollection = &anilist.AnimeCollection{
				MediaListCollection: &anilist.AnimeCollection_MediaListCollection{
					Lists: []*anilist.AnimeCollection_MediaListCollection_Lists{},
				},
			}
		}
	}

	// Resolve media: prefer the collection's entry (which carries list data), else
	// fall back to getAnime() which caches and can fetch from the platform.
	var media *anilist.BaseAnime
	if listEntry, ok := animeCollection.GetListEntryFromAnimeId(mId); ok {
		media = listEntry.Media
	}
	if media == nil {
		fetched, err := m.getAnime(ctx, mId)
		if err != nil {
			return fmt.Errorf("cannot play local file, could not fetch media %d: %w", mId, err)
		}
		media = fetched
	}
	if media == nil {
		return fmt.Errorf("media not found: %d", mId)
	}

	episodeCollection, err := anime.NewEpisodeCollectionFromLocalFiles(ctx, anime.NewEpisodeCollectionFromLocalFilesOptions{
		LocalFiles:          opts.LocalFiles,
		Media:               media,
		AnimeCollection:     animeCollection,
		PlatformRef:         m.platformRef,
		MetadataProviderRef: m.metadataProviderRef,
		Logger:              m.Logger,
	})
	if err != nil {
		return fmt.Errorf("cannot play local file, could not create episode collection: %w", err)
	}

	var episode *anime.Episode
	for _, e := range episodeCollection.Episodes {
		if e.LocalFile != nil && util.NormalizePath(e.LocalFile.Path) == util.NormalizePath(lf.Path) {
			episode = e
			break
		}
	}

	if episode == nil {
		return fmt.Errorf("cannot play local file, could not find episode for local file: %s", opts.Path)
	}

	stream := &LocalFileStream{
		localFile: lf,
		BaseStream: BaseStream{
			manager:               m,
			logger:                m.Logger,
			clientId:              opts.ClientId,
			filename:              filepath.Base(lf.Path),
			media:                 media,
			episode:               episode,
			episodeCollection:     episodeCollection,
			subtitleEventCache:    result.NewMap[string, *mkvparser.SubtitleEvent](),
			activeSubtitleStreams: result.NewMap[string, *SubtitleStream](),
		},
	}

	go func() {
		m.loadStream(stream)
	}()

	return nil
}
