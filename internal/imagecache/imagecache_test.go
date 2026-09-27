package imagecache

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"seanime/internal/util"

	"github.com/stretchr/testify/require"
)

// imageServer serves a small JPEG and counts requests.
func imageServer(t *testing.T, contentType string, body []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// newTestCache uses the test server's client: the real client refuses 127.0.0.1 by design.
func newTestCache(t *testing.T, client *http.Client) *Cache {
	t.Helper()
	c, err := newCache(t.TempDir(), DefaultMaxMB, client, util.NewLogger())
	require.NoError(t, err)
	return c
}

func TestMissFetchesAndSavesThenHitSkipsNetwork(t *testing.T) {
	srv, hits := imageServer(t, "image/jpeg", []byte("jpegbytes"))
	c := newTestCache(t, srv.Client())

	body, ct, err := c.Get(context.Background(), srv.URL+"/cover.jpg")
	require.NoError(t, err)
	require.Equal(t, "jpegbytes", string(body))
	require.Equal(t, "image/jpeg", ct)

	body, ct, err = c.Get(context.Background(), srv.URL+"/cover.jpg")
	require.NoError(t, err)
	require.Equal(t, "jpegbytes", string(body))
	require.Equal(t, "image/jpeg", ct)
	require.EqualValues(t, 1, hits.Load(), "second request served from disk")
}

func TestContentTypeParametersAccepted(t *testing.T) {
	srv, _ := imageServer(t, "image/png; charset=binary", []byte("png"))
	c := newTestCache(t, srv.Client())
	_, ct, err := c.Get(context.Background(), srv.URL+"/a.png")
	require.NoError(t, err)
	require.Equal(t, "image/png; charset=binary", ct)
}

func TestRefusesNonImagesAndSVG(t *testing.T) {
	for _, ct := range []string{"text/html", "image/svg+xml", ""} {
		srv, _ := imageServer(t, ct, []byte("<svg/>"))
		c := newTestCache(t, srv.Client())
		_, _, err := c.Get(context.Background(), srv.URL+"/x")
		require.ErrorIs(t, err, ErrNotImage, ct)
		require.EqualValues(t, 0, c.Size(), "refused responses aren't saved")
	}
}

func TestRefusesOversizedImages(t *testing.T) {
	srv, _ := imageServer(t, "image/jpeg", make([]byte, maxImageBytes+1))
	c := newTestCache(t, srv.Client())
	_, _, err := c.Get(context.Background(), srv.URL+"/big.jpg")
	require.ErrorIs(t, err, ErrTooLarge)
}

func TestRefusesNonOKResponses(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	c := newTestCache(t, srv.Client())
	_, _, err := c.Get(context.Background(), srv.URL+"/missing.jpg")
	require.Error(t, err)
}

func TestRefusesNonHTTPURLs(t *testing.T) {
	c := newTestCache(t, http.DefaultClient)
	for _, u := range []string{"file:///etc/passwd", "ftp://example.com/a.jpg", "not a url", ""} {
		_, _, err := c.Get(context.Background(), u)
		require.ErrorIs(t, err, ErrInvalidURL, u)
	}
}

// The real client must refuse home-network addresses, including the loopback test server.
func TestPublicOnlyClientRefusesPrivateAddresses(t *testing.T) {
	srv, hits := imageServer(t, "image/jpeg", []byte("x"))
	c, err := New(t.TempDir(), DefaultMaxMB, util.NewLogger())
	require.NoError(t, err)

	_, _, err = c.Get(context.Background(), srv.URL+"/a.jpg")
	require.ErrorIs(t, err, ErrPrivateAddress)
	require.EqualValues(t, 0, hits.Load())
}

func TestCorruptEntryIsRefetched(t *testing.T) {
	srv, hits := imageServer(t, "image/jpeg", []byte("jpegbytes"))
	c := newTestCache(t, srv.Client())
	u := srv.URL + "/a.jpg"
	require.NoError(t, c.store.Put(u, []byte("no-header-line")))

	body, _, err := c.Get(context.Background(), u)
	require.NoError(t, err)
	require.Equal(t, "jpegbytes", string(body))
	require.EqualValues(t, 1, hits.Load())
}

// Review focus 1: a burst of requests for one new image fetches it once and all succeed.
func TestConcurrentMissesFetchOnce(t *testing.T) {
	release := make(chan struct{})
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-release
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpegbytes"))
	}))
	t.Cleanup(srv.Close)
	c := newTestCache(t, srv.Client())

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := c.Get(context.Background(), srv.URL+"/shared.jpg")
			errs <- err
		}()
	}
	for hits.Load() == 0 {
		// wait until the first request reaches the server
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, hits.Load())
}

// One caller giving up on a shared fetch must not cancel it for the others: the fetch runs
// detached from any one caller's context.
func TestAbandonedCallerDoesNotCancelOthers(t *testing.T) {
	release := make(chan struct{})
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-release
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpegbytes"))
	}))
	t.Cleanup(srv.Close)
	c := newTestCache(t, srv.Client())
	u := srv.URL + "/shared.jpg"

	ctxA, cancelA := context.WithCancel(context.Background())
	errA := make(chan error, 1)
	go func() {
		_, _, err := c.Get(ctxA, u)
		errA <- err
	}()

	require.Eventually(t, func() bool { return hits.Load() == 1 }, time.Second, time.Millisecond)

	var bodyB, ctB atomic.Value
	errB := make(chan error, 1)
	go func() {
		body, ct, err := c.Get(context.Background(), u)
		bodyB.Store(body)
		ctB.Store(ct)
		errB <- err
	}()

	cancelA()
	require.ErrorIs(t, <-errA, context.Canceled)

	close(release)
	require.NoError(t, <-errB)
	require.Equal(t, "jpegbytes", string(bodyB.Load().([]byte)))
	require.Equal(t, "image/jpeg", ctB.Load().(string))
	require.EqualValues(t, 1, hits.Load())
}

func TestSetMaxMB(t *testing.T) {
	c := newTestCache(t, http.DefaultClient)
	require.Error(t, c.SetMaxMB(MinMaxMB-1))
	require.NoError(t, c.SetMaxMB(2048))
	require.Equal(t, 2048, c.MaxMB())
}

// Review focus 3: lowering the limit shrinks the cache right away.
func TestSetMaxMBTrimsImmediately(t *testing.T) {
	c := newTestCache(t, http.DefaultClient)
	chunk := strings.Repeat("x", 1<<20)
	for i := 0; i < 150; i++ {
		require.NoError(t, c.store.Put(string(rune('a'+i%26))+strings.Repeat("k", i), []byte("image/jpeg\n"+chunk)))
	}
	require.Greater(t, c.Size(), int64(MinMaxMB)<<20)

	require.NoError(t, c.SetMaxMB(MinMaxMB))
	require.LessOrEqual(t, c.Size(), int64(MinMaxMB)<<20)
}

func TestNewClampsLimitToMinimum(t *testing.T) {
	c, err := newCache(t.TempDir(), 5, http.DefaultClient, util.NewLogger())
	require.NoError(t, err)
	require.Equal(t, MinMaxMB, c.MaxMB())
}
