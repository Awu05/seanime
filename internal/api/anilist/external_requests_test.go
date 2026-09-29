package anilist

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIsAPIURLMatchesTheEndpointInUse(t *testing.T) {
	prevProvider := CurrentRequestProvider()
	t.Cleanup(func() { require.NoError(t, SetRequestProvider(prevProvider)) })

	UseOfficialAPI()
	require.True(t, IsAPIURL("https://graphql.anilist.co"))
	require.True(t, IsAPIURL("https://GraphQL.AniList.co/"))
	require.False(t, IsAPIURL("https://anilist.co/api/v2/oauth/token"))
	require.False(t, IsAPIURL("://bad"))

	require.NoError(t, UseCustomAPI(CustomClientConfig{Name: "mirror", Endpoint: "https://mirror.example.com/graphql"}))
	require.True(t, IsAPIURL("https://mirror.example.com/graphql"))
	require.False(t, IsAPIURL("https://graphql.anilist.co"), "only the endpoint in use shares its limit")
}

func TestPaceExternalRequestStopsWhenCancelled(t *testing.T) {
	prevProvider := CurrentRequestProvider()
	t.Cleanup(func() { require.NoError(t, SetRequestProvider(prevProvider)) })
	UseOfficialAPI()
	sharedAniListPacer.BlockUntil(time.Now().Add(time.Minute))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	require.ErrorIs(t, PaceExternalRequest(ctx), context.Canceled)
	require.Less(t, time.Since(start), time.Second)
}
