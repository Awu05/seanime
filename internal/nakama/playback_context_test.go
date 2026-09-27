package nakama

import (
	"seanime/internal/videocore"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPartyPlaybackFollowsTheBoundSession guards watch parties on a multi-profile server: they
// used the app-wide players, which no profile's browser plays through, so the host announced no
// stream and never saw its own playback. A party must use the session of the profile running it.
func TestPartyPlaybackFollowsTheBoundSession(t *testing.T) {
	appWide := PlaybackContext{VideoCore: &videocore.VideoCore{}}
	m := &Manager{defaultPlayback: appWide}
	require.Same(t, appWide.VideoCore, m.PartyPlayback().VideoCore)

	hostSession := PlaybackContext{VideoCore: &videocore.VideoCore{}}
	m.BindPartyPlayback(hostSession)
	require.Same(t, hostSession.VideoCore, m.PartyPlayback().VideoCore)
	require.Same(t, appWide.VideoCore, m.DefaultPlayback().VideoCore)
}
