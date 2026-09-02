package core

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTorrentstreamInitFailureToastMessage(t *testing.T) {
	err := errors.New("error creating a new torrent client: first listen: listen tcp4 :8084: bind: address already in use")
	msg := torrentstreamInitFailureToastMessage(err)

	// The port-conflict reason must reach the user - this previously only ever appeared in the
	// docker logs, so a bad port config (e.g. reusing qBittorrent's WebUI port) silently broke
	// every torrent stream with no indication why.
	assert.Contains(t, msg, "address already in use")
	assert.Contains(t, msg, "Torrent streaming")
}
