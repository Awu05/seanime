package simkl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestPin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/oauth/pin", r.URL.Path)
		assert.Equal(t, "test-client-id", r.URL.Query().Get("client_id"))
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"result":           "OK",
			"device_code":      "DEVICE_CODE",
			"user_code":        "5G6JAH",
			"verification_uri": "https://simkl.com/pin",
			"expires_in":       900,
			"interval":         5,
		})
	}))
	defer server.Close()

	pin, err := requestPinAt(context.Background(), server.Client(), server.URL, "test-client-id")
	require.NoError(t, err)
	assert.Equal(t, "5G6JAH", pin.UserCode)
	assert.Equal(t, "https://simkl.com/pin", pin.VerificationURI)
	assert.Equal(t, 900, pin.ExpiresIn)
	assert.Equal(t, 5, pin.Interval)
}

func TestPollPin_Pending(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/oauth/pin/5G6JAH", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"result":  "KO",
			"message": "Authorization pending",
		})
	}))
	defer server.Close()

	token, done, err := pollPinAt(context.Background(), server.Client(), server.URL, "test-client-id", "5G6JAH")
	require.NoError(t, err)
	assert.False(t, done)
	assert.Empty(t, token)
}

func TestPollPin_Complete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"result":       "OK",
			"access_token": "the-access-token",
		})
	}))
	defer server.Close()

	token, done, err := pollPinAt(context.Background(), server.Client(), server.URL, "test-client-id", "5G6JAH")
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, "the-access-token", token)
}
