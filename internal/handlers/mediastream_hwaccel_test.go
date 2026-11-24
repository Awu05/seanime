package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

// Invalid custom hwaccel settings must be rejected when saved, not discovered later as a silent
// CPU fallback during transcoding.
func TestHandleSaveMediastreamSettingsRejectsInvalidCustomHwAccel(t *testing.T) {
	body := `{"settings":{"transcodeHwAccel":"custom","transcodeHwAccelCustomSettings":"{\"encodeFlags\":[\"-f\",\"mpegts\",\"-y\",\"/tmp/x\"],\"scaleFilter\":\"scale=%d:%d\"}"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/mediastream/settings", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()

	h := &Handler{}
	err := h.HandleSaveMediastreamSettings(echo.New().NewContext(req, rec))
	assert.NoError(t, err)
	assert.Contains(t, rec.Body.String(), "unsupported ffmpeg option")
}
