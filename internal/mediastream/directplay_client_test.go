package mediastream

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"seanime/internal/database/models"
	"seanime/internal/events"
	"seanime/internal/mediastream/videofile"
	"seanime/internal/util"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newDirectPlayTestRepository returns an initialized repository with a cached
// media container for a real file, so requesting playback needs no ffprobe.
func newDirectPlayTestRepository(t *testing.T) (*Repository, string) {
	t.Helper()
	logger := util.NewLogger()
	r := NewRepository(&NewRepositoryOptions{
		Logger:         logger,
		WSEventManager: events.NewMockWSEventManager(logger),
	})
	r.settings = mo.Some(&models.MediastreamSettings{})

	file := filepath.Join(t.TempDir(), "movie.mkv")
	require.NoError(t, os.WriteFile(file, []byte("video bytes"), 0o644))
	hash, err := videofile.GetHashFromPath(file)
	require.NoError(t, err)
	r.playbackManager.mediaContainers.Set(hash, &MediaContainer{
		Filepath:   file,
		Hash:       hash,
		StreamType: StreamTypeDirect,
		StreamUrl:  "/api/v1/mediastream/direct",
	})
	return r, file
}

func serveDirectPlay(t *testing.T, r *Repository, clientId string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/mediastream/direct", nil)
	rec := httptest.NewRecorder()
	err := r.ServeEchoDirectPlay(echo.New().NewContext(req, rec), clientId)
	return rec, err
}

// Direct play stream URLs must carry the client ID the file was loaded for, like
// transcode URLs do, so each tab's <video> requests find its own file.
func TestRequestDirectPlayStreamUrlCarriesClientId(t *testing.T) {
	r, file := newDirectPlayTestRepository(t)

	mc, err := r.RequestDirectPlay(file, "tab a")
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/mediastream/direct?clientId=tab+a", mc.StreamUrl)

	rec, err := serveDirectPlay(t, r, "tab a")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "video bytes", rec.Body.String())

	_, err = serveDirectPlay(t, r, "default")
	assert.EqualError(t, err, "no file has been loaded")
	assert.True(t, r.HasMediaContainer("tab a"))
	assert.False(t, r.HasMediaContainer("default"))
}

func TestRequestTranscodeStreamUrlKeepsSingleClientId(t *testing.T) {
	r, file := newDirectPlayTestRepository(t)
	hash, _ := videofile.GetHashFromPath(file)
	r.playbackManager.mediaContainers.Set(hash, &MediaContainer{
		Filepath:   file,
		Hash:       hash,
		StreamType: StreamTypeTranscode,
		StreamUrl:  "/api/v1/mediastream/transcode/master.m3u8",
	})

	mc, err := r.playbackManager.RequestPlayback(file, StreamTypeTranscode, "tab-a")
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/mediastream/transcode/master.m3u8?clientId=tab-a", mc.StreamUrl)

	// the cached container is shared across clients and must not be modified
	cached, _ := r.playbackManager.mediaContainers.Get(hash)
	assert.Equal(t, "/api/v1/mediastream/transcode/master.m3u8", cached.StreamUrl)
}
