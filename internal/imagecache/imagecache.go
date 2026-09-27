// Package imagecache saves images on disk the first time they're requested, so covers, banners
// and episode thumbnails still show when the internet is down.
package imagecache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"seanime/internal/security"
	"seanime/internal/util/diskstore"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/sync/singleflight"
)

const (
	DefaultMaxMB  = 1024
	MinMaxMB      = 100
	MaxMaxMB      = 1 << 20 // 1 TB
	maxImageBytes = 10 << 20
	fetchTimeout  = 15 * time.Second
)

var (
	ErrInvalidURL     = errors.New("imagecache: not an http(s) URL")
	ErrNotImage       = errors.New("imagecache: response is not a supported image")
	ErrTooLarge       = errors.New("imagecache: image is larger than 10 MB")
	ErrPrivateAddress = errors.New("imagecache: refusing to connect to a private network address")
)

type Cache struct {
	store  *diskstore.Store
	client *http.Client
	maxMB  atomic.Int64
	group  singleflight.Group
	logger *zerolog.Logger
}

// New opens the image cache at dir with a limit of maxMB (raised to MinMaxMB if lower).
func New(dir string, maxMB int, logger *zerolog.Logger) (*Cache, error) {
	return newCache(dir, maxMB, publicOnlyClient(), logger)
}

func newCache(dir string, maxMB int, client *http.Client, logger *zerolog.Logger) (*Cache, error) {
	c := &Cache{client: client, logger: logger}
	c.maxMB.Store(int64(min(max(maxMB, MinMaxMB), MaxMaxMB)))
	store, err := diskstore.New(dir, func() int64 { return c.maxMB.Load() << 20 }, logger)
	if err != nil {
		return nil, err
	}
	c.store = store
	return c, nil
}

// publicOnlyClient refuses to connect to anything but public internet addresses. The check runs on
// the address actually dialled, so redirects and DNS rebinding can't reach the home network either.
// Proxy env vars are ignored so the check always sees the real destination.
func publicOnlyClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			addr, err := netip.ParseAddr(host)
			if err != nil {
				return err
			}
			if security.IsPrivateNetworkAddr(addr) {
				return ErrPrivateAddress
			}
			return nil
		},
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialer.DialContext
	return &http.Client{Transport: transport}
}

func (c *Cache) MaxMB() int { return int(c.maxMB.Load()) }

// SetMaxMB changes the limit and trims right away if the cache is now over it.
func (c *Cache) SetMaxMB(mb int) error {
	if mb < MinMaxMB {
		return fmt.Errorf("imagecache: limit must be at least %d MB", MinMaxMB)
	}
	if mb > MaxMaxMB {
		return fmt.Errorf("imagecache: limit must be at most %d MB", MaxMaxMB)
	}
	c.maxMB.Store(int64(mb))
	c.store.Trim()
	return nil
}

func (c *Cache) Size() int64 { return c.store.Size() }

func (c *Cache) Clear() error { return c.store.Clear() }

type image struct {
	body        []byte
	contentType string
}

// Get returns the image at rawURL and its content type: the saved copy when there is one,
// otherwise fetched, saved and returned. Image URLs from AniList and TVDB never change content,
// so a saved copy is never refetched. Concurrent misses for one URL share a single fetch, which
// runs detached from any one caller's context (fetch has its own timeout) so one caller giving up
// - a lazy-loaded cover scrolled off-screen, a navigation - can't cancel the fetch for the rest.
func (c *Cache) Get(ctx context.Context, rawURL string) ([]byte, string, error) {
	if img, ok := c.saved(rawURL); ok {
		return img.body, img.contentType, nil
	}
	ch := c.group.DoChan(rawURL, func() (interface{}, error) {
		if img, ok := c.saved(rawURL); ok {
			return img, nil
		}
		img, err := c.fetch(context.WithoutCancel(ctx), rawURL)
		if err != nil {
			return nil, err
		}
		if err := c.store.Put(rawURL, encode(img)); err != nil {
			c.logger.Warn().Err(err).Msg("imagecache: Could not save image")
		}
		return img, nil
	})
	select {
	case res := <-ch:
		if res.Err != nil {
			return nil, "", res.Err
		}
		img := res.Val.(image)
		return img.body, img.contentType, nil
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
}

func (c *Cache) saved(rawURL string) (image, bool) {
	data, ok := c.store.Get(rawURL)
	if !ok {
		return image{}, false
	}
	img, ok := decode(data)
	if !ok {
		c.store.Delete(rawURL)
	}
	return img, ok
}

func (c *Cache) fetch(ctx context.Context, rawURL string) (image, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return image{}, ErrInvalidURL
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return image{}, ErrInvalidURL
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return image{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return image{}, fmt.Errorf("imagecache: upstream returned %d", resp.StatusCode)
	}
	contentType := resp.Header.Get("Content-Type")
	if !isAllowedImageType(contentType) {
		return image{}, ErrNotImage
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return image{}, err
	}
	if len(body) > maxImageBytes {
		return image{}, ErrTooLarge
	}
	return image{body: body, contentType: contentType}, nil
}

// isAllowedImageType accepts image types except SVG, which could run script when served from
// Seanime's own origin.
func isAllowedImageType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && strings.HasPrefix(mediaType, "image/") && mediaType != "image/svg+xml"
}

// encode stores the content type on the first line, followed by the image bytes.
func encode(img image) []byte {
	return append([]byte(img.contentType+"\n"), img.body...)
}

func decode(data []byte) (image, bool) {
	i := bytes.IndexByte(data, '\n')
	if i <= 0 {
		return image{}, false
	}
	contentType := string(data[:i])
	if !isAllowedImageType(contentType) {
		return image{}, false
	}
	return image{body: data[i+1:], contentType: contentType}, true
}
