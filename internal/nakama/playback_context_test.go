package nakama

import (
	"seanime/internal/videocore"
	"testing"

	"github.com/samber/mo"
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

// TestIsPartyPlayingThrough guards the host session staying alive for the whole party: only the
// core a running party is bound to may be kept from idle eviction.
func TestIsPartyPlayingThrough(t *testing.T) {
	m := &Manager{}
	m.watchPartyManager = NewWatchPartyManager(m)
	hostCore, otherCore := &videocore.VideoCore{}, &videocore.VideoCore{}
	m.BindPartyPlayback(PlaybackContext{VideoCore: hostCore})

	require.False(t, m.IsPartyPlayingThrough(hostCore), "no party is running yet")

	m.watchPartyManager.currentSession = mo.Some(&WatchPartySession{})
	require.True(t, m.IsPartyPlayingThrough(hostCore))
	require.False(t, m.IsPartyPlayingThrough(otherCore))
}

// TestStreamTokenParam guards watch party streams opened in a desktop player on a multi-profile
// server: the player can't send the login cookie, so the stream URL must carry the token.
func TestStreamTokenParam(t *testing.T) {
	require.Equal(t, "&auth_token=a%2Bb", streamTokenParam(PlaybackContext{StreamToken: func() string { return "a+b" }}))
	require.Empty(t, streamTokenParam(PlaybackContext{StreamToken: func() string { return "" }}))
	require.Empty(t, streamTokenParam(PlaybackContext{}))
}
