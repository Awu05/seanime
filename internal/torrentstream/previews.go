package torrentstream

import (
	"context"
	"fmt"
	"path/filepath"
	"seanime/internal/api/anilist"
	hibiketorrent "seanime/internal/extension/hibike/torrent"
	"seanime/internal/util"
	"seanime/internal/util/comparison"

	"github.com/5rahim/habari"
)

type (
	FilePreview struct {
		Path                  string `json:"path"`
		DisplayPath           string `json:"displayPath"`
		DisplayTitle          string `json:"displayTitle"`
		EpisodeNumber         int    `json:"episodeNumber"`
		RelativeEpisodeNumber int    `json:"relativeEpisodeNumber"`
		IsLikely              bool   `json:"isLikely"`
		Index                 int    `json:"index"`
	}

	GetTorrentFilePreviewsOptions struct {
		Torrent        *hibiketorrent.AnimeTorrent
		Magnet         string
		EpisodeNumber  int
		AbsoluteOffset int
		Media          *anilist.BaseAnime
	}
)

func (r *Repository) GetTorrentFilePreviewsFromManualSelection(opts *GetTorrentFilePreviewsOptions) (ret []*FilePreview, err error) {
	defer util.HandlePanicInModuleWithError("torrentstream/GetTorrentFilePreviewsFromManualSelection", &err)

	if opts.Torrent == nil || opts.Magnet == "" || opts.Media == nil {
		return nil, fmt.Errorf("torrentstream: Invalid options")
	}

	r.logger.Trace().Str("hash", opts.Torrent.InfoHash).Msg("torrentstream: Getting file previews for torrent selection")

	selectedTorrent, err := r.client.AddTorrent(context.Background(), opts.Magnet)
	if err != nil {
		r.logger.Error().Err(err).Msgf("torrentstream: Error adding torrent %s", opts.Magnet)
		return nil, err
	}

	files := selectedTorrent.Files()
	fileMetadata := make([]*habari.Metadata, len(files))
	containsAbsoluteEps := false
	for i, file := range files {
		fileMetadata[i] = habari.Parse(filepath.Base(file.Path()))
		if len(fileMetadata[i].EpisodeNumber) == 1 && util.StringToIntMust(fileMetadata[i].EpisodeNumber[0]) > opts.Media.GetTotalEpisodeCount() {
			containsAbsoluteEps = true
		}
	}

	ret = make([]*FilePreview, len(files))
	for i, file := range files {
		metadata := fileMetadata[i]
		displayTitle := filepath.Base(file.Path())
		parsedEpisodeNumber := -1

		if len(metadata.EpisodeNumber) == 1 && !comparison.ValueContainsSpecial(displayTitle) && !comparison.ValueContainsNC(displayTitle) {
			parsedEpisodeNumber = util.StringToIntMust(metadata.EpisodeNumber[0])
			displayTitle = fmt.Sprintf("Episode %d", parsedEpisodeNumber)
			if metadata.EpisodeTitle != "" {
				displayTitle = fmt.Sprintf("%s - %s", displayTitle, metadata.EpisodeTitle)
			}
		}

		ret[i] = &FilePreview{
			Path:          file.Path(),
			DisplayPath:   filepath.Base(file.Path()),
			DisplayTitle:  displayTitle,
			EpisodeNumber: parsedEpisodeNumber,
			IsLikely:      !containsAbsoluteEps && parsedEpisodeNumber == opts.EpisodeNumber,
			Index:         i,
		}
	}

	r.logger.Debug().Str("hash", opts.Torrent.InfoHash).Msg("torrentstream: Got file previews for torrent selection, dropping torrent")
	go r.client.dropIfUnclaimed(selectedTorrent)

	return
}
