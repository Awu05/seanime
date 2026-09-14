package events

import (
	"testing"

	"seanime/internal/util"

	"github.com/stretchr/testify/require"
)

func TestWSEventManagerGetClientPlatform(t *testing.T) {
	manager := NewWSEventManager(util.NewLogger())

	manager.AddConn("web-client", "", nil)
	manager.AddConn("denshi-client", "", nil, "denshi")

	require.Empty(t, manager.GetClientPlatform("web-client"))
	require.Equal(t, "denshi", manager.GetClientPlatform("denshi-client"))
	require.Empty(t, manager.GetClientPlatform("missing-client"))
}

func TestWSEventManagerGetConnections(t *testing.T) {
	manager := NewWSEventManager(util.NewLogger())

	manager.AddConn("web-client", "profile-1", nil)
	manager.AddConn("denshi-client", "profile-2", nil, "denshi")

	conns := manager.GetConnections()
	require.Len(t, conns, 2)

	byID := map[string]WSConnDTO{}
	for _, c := range conns {
		byID[c.ID] = c
	}

	require.Equal(t, "profile-1", byID["web-client"].ProfileID)
	require.Empty(t, byID["web-client"].Platform)
	require.False(t, byID["web-client"].ConnectedAt.IsZero(), "expected ConnectedAt to be set on connect")

	require.Equal(t, "profile-2", byID["denshi-client"].ProfileID)
	require.Equal(t, "denshi", byID["denshi-client"].Platform)
}
