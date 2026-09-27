package directstream

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestContentTypeFromPath guards the labels browsers get for streamed files: MKV must go out as
// WebM (which browsers accept for Matroska), and an unknown extension as "" so the caller sniffs.
func TestContentTypeFromPath(t *testing.T) {
	require.Equal(t, "video/webm", ContentTypeFromPath("Show/[Group] Show - 01.mkv"))
	require.Equal(t, "video/mp4", ContentTypeFromPath("Show - 01.mp4"))
	require.Empty(t, ContentTypeFromPath("Show - 01"))
}
