package anilist

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQueryOperationName(t *testing.T) {
	require.Equal(t, "ListAnime", queryOperationName([]byte(`{"query":"query ListAnime($page: Int) { Page { media { id } } }"}`)))
	require.Equal(t, "UpdateEntry", queryOperationName([]byte(`{"query":"mutation UpdateEntry { SaveMediaListEntry { id } }"}`)))
	require.Equal(t, "", queryOperationName([]byte(`{"query":"{ Viewer { id } }"}`)))
	require.Equal(t, "", queryOperationName([]byte(`not json`)))
}
