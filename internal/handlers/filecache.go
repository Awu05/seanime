package handlers

import (
	"net/http"
	"path/filepath"
	"seanime/internal/core"
	"seanime/internal/util"
	"strings"

	"github.com/labstack/echo/v4"
)

// HandleGetFileCacheTotalSize
//
//	@summary returns the total size of cache files.
//	@desc The total size of the cache files is returned in human-readable format.
//	@route /api/v1/filecache/total-size [GET]
//	@returns string
func (h *Handler) HandleGetFileCacheTotalSize(c echo.Context) error {
	// The saved episode info and cached images live under the same cache directory but are shown
	// and cleared from their own "Offline copies" card, so exclude them here.
	size, err := h.App.FileCacher.GetTotalSize(filepath.Join(h.App.Config.Cache.Dir, core.OfflineCopiesDirName))
	if err != nil {
		return h.RespondWithError(c, err)
	}

	// Return the cache size
	return h.RespondWithData(c, util.Bytes(uint64(size)))
}

// HandleRemoveFileCacheBucket
//
//	@summary deletes all buckets with the given prefix.
//	@desc The bucket value is the prefix of the cache files that should be deleted.
//	@desc Returns 'true' if the operation was successful.
//	@route /api/v1/filecache/bucket [DELETE]
//	@returns bool
func (h *Handler) HandleRemoveFileCacheBucket(c echo.Context) error {
	if err := h.guardStrictLocalOnlyAction(c); err != nil {
		return err
	}

	type body struct {
		Bucket string `json:"bucket"` // e.g. "onlinestream_"
	}

	// Parse the request body
	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	// Remove all files in the cache directory that match the given filter
	err := h.App.FileCacher.RemoveAllBy(func(filename string) bool {
		return strings.HasPrefix(filename, b.Bucket)
	})

	if err != nil {
		return h.RespondWithError(c, err)
	}

	// Return a success response
	return h.RespondWithData(c, true)
}

// HandleGetFileCacheMediastreamVideoFilesTotalSize
//
//	@summary returns the total size of cached video file data.
//	@desc The total size of the cache video file data is returned in human-readable format.
//	@route /api/v1/filecache/mediastream/videofiles/total-size [GET]
//	@returns string
func (h *Handler) HandleGetFileCacheMediastreamVideoFilesTotalSize(c echo.Context) error {
	// Get the cache size
	size, err := h.App.FileCacher.GetMediastreamVideoFilesTotalSize()
	if err != nil {
		return h.RespondWithError(c, err)
	}

	// Return the cache size
	return h.RespondWithData(c, util.Bytes(uint64(size)))
}

// HandleClearFileCacheMediastreamVideoFiles
//
//	@summary deletes the contents of the mediastream video file cache directory.
//	@desc Returns 'true' if the operation was successful.
//	@route /api/v1/filecache/mediastream/videofiles [DELETE]
//	@returns bool
func (h *Handler) HandleClearFileCacheMediastreamVideoFiles(c echo.Context) error {
	if err := h.guardStrictLocalOnlyAction(c); err != nil {
		return err
	}

	// Clear the attachments
	err := h.App.FileCacher.ClearMediastreamVideoFiles()

	if err != nil {
		return h.RespondWithError(c, err)
	}

	// Clear the transcode dir
	h.App.MediastreamRepository.ClearTranscodeDir()

	if h.App.MediastreamRepository != nil {
		go h.App.MediastreamRepository.CacheWasCleared()
	}

	// Return a success response
	return h.RespondWithData(c, true)
}

// canManageSharedCaches reports whether the request may manage caches every profile shares:
// anyone on a single-user install, only the admin once there are profiles.
func (h *Handler) canManageSharedCaches(c echo.Context) bool {
	return !h.App.MultiUserEnabled || core.GetIsAdminFromContext(c)
}

func respondAdminRequired(c echo.Context) error {
	return c.JSON(http.StatusForbidden, map[string]string{"error": "Admin access required"})
}

// OfflineCopiesInfo describes the saved episode info, title records and images that keep the library complete
// during an outage.
type OfflineCopiesInfo struct {
	TotalSize       string `json:"totalSize"`
	ImageCacheMaxMB int    `json:"imageCacheMaxMB"`
}

// HandleGetOfflineCopies
//
//	@summary returns the size of saved episode info, title records and images, and the image cache limit.
//	@route /api/v1/filecache/offline-copies [GET]
//	@returns handlers.OfflineCopiesInfo
func (h *Handler) HandleGetOfflineCopies(c echo.Context) error {
	if !h.canManageSharedCaches(c) {
		return respondAdminRequired(c)
	}
	size := h.App.EpisodeInfoStore.Size() + h.App.ImageCache.Size() + h.App.TitleCache.Size()
	return h.RespondWithData(c, OfflineCopiesInfo{
		TotalSize:       util.Bytes(uint64(size)),
		ImageCacheMaxMB: h.App.ImageCache.MaxMB(),
	})
}

// HandleSetImageCacheLimit
//
//	@summary sets the image cache size limit in MB.
//	@desc The cache is trimmed right away if it's over the new limit.
//	@route /api/v1/filecache/offline-copies/limit [POST]
//	@returns bool
func (h *Handler) HandleSetImageCacheLimit(c echo.Context) error {
	if !h.canManageSharedCaches(c) {
		return respondAdminRequired(c)
	}
	if err := h.guardStrictLocalOnlyAction(c); err != nil {
		return err
	}
	type body struct {
		ImageCacheMaxMB int `json:"imageCacheMaxMB"`
	}
	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}
	if err := h.App.SetImageCacheMaxMB(b.ImageCacheMaxMB); err != nil {
		return h.RespondWithError(c, err)
	}
	return h.RespondWithData(c, true)
}

// HandleClearOfflineCopies
//
//	@summary deletes all saved episode info, title records and images.
//	@route /api/v1/filecache/offline-copies [DELETE]
//	@returns bool
func (h *Handler) HandleClearOfflineCopies(c echo.Context) error {
	if !h.canManageSharedCaches(c) {
		return respondAdminRequired(c)
	}
	if err := h.guardStrictLocalOnlyAction(c); err != nil {
		return err
	}
	if err := h.App.EpisodeInfoStore.Clear(); err != nil {
		return h.RespondWithError(c, err)
	}
	if err := h.App.ImageCache.Clear(); err != nil {
		return h.RespondWithError(c, err)
	}
	if err := h.App.TitleCache.Clear(); err != nil {
		return h.RespondWithError(c, err)
	}
	return h.RespondWithData(c, true)
}
