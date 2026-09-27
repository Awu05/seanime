package diskstore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"seanime/internal/util"

	"github.com/stretchr/testify/require"
)

func newStore(t *testing.T, maxBytes int64) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := New(dir, func() int64 { return maxBytes }, util.NewLogger())
	require.NoError(t, err)
	return s, dir
}

// setAge backdates an entry so trimming and read-refresh tests don't need to sleep.
func setAge(t *testing.T, s *Store, key string, age time.Duration) {
	t.Helper()
	at := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(s.path(key), at, at))
}

func TestPutGetRoundTrip(t *testing.T) {
	s, _ := newStore(t, 1<<20)
	require.NoError(t, s.Put("a", []byte("hello")))

	got, ok := s.Get("a")
	require.True(t, ok)
	require.Equal(t, "hello", string(got))

	_, ok = s.Get("missing")
	require.False(t, ok)
}

// Long keys (image URLs with query strings) must still map to a valid file name.
func TestLongKey(t *testing.T) {
	s, _ := newStore(t, 1<<20)
	key := "https://example.com/" + strings.Repeat("x", 5000)
	require.NoError(t, s.Put(key, []byte("v")))
	got, ok := s.Get(key)
	require.True(t, ok)
	require.Equal(t, "v", string(got))
}

func TestSizeTracksOverwriteAndDelete(t *testing.T) {
	s, _ := newStore(t, 1<<20)
	require.NoError(t, s.Put("a", make([]byte, 100)))
	require.NoError(t, s.Put("a", make([]byte, 40)))
	require.EqualValues(t, 40, s.Size())

	s.Delete("a")
	require.EqualValues(t, 0, s.Size())
	_, ok := s.Get("a")
	require.False(t, ok)
}

// Going over the limit deletes the least recently used entries until the store is at 90%.
func TestPutTrimsLeastRecentlyUsed(t *testing.T) {
	s, _ := newStore(t, 1000)
	for i := 0; i < 4; i++ {
		key := fmt.Sprintf("k%d", i)
		require.NoError(t, s.Put(key, make([]byte, 250)))
		setAge(t, s, key, time.Duration(10-i)*time.Hour) // k0 oldest
	}
	require.EqualValues(t, 1000, s.Size())

	require.NoError(t, s.Put("new", make([]byte, 250))) // 1250 > 1000 → trim to <= 900

	require.LessOrEqual(t, s.Size(), int64(900))
	_, ok := s.Get("k0")
	require.False(t, ok, "oldest entry trimmed first")
	_, ok = s.Get("k1")
	require.False(t, ok)
	_, ok = s.Get("new")
	require.True(t, ok, "the entry just written survives")
}

// Review focus 3: lowering the limit takes effect as soon as Trim runs, not only on the next Put.
func TestTrimAppliesLoweredLimit(t *testing.T) {
	limit := int64(1000)
	dir := t.TempDir()
	s, err := New(dir, func() int64 { return limit }, util.NewLogger())
	require.NoError(t, err)
	for i := 0; i < 4; i++ {
		require.NoError(t, s.Put(fmt.Sprintf("k%d", i), make([]byte, 250)))
	}

	limit = 500
	s.Trim()
	require.LessOrEqual(t, s.Size(), int64(450))
}

// A read refreshes an entry's age so it's trimmed last, but at most once a day.
func TestGetRefreshesAgeAtMostDaily(t *testing.T) {
	s, _ := newStore(t, 1<<20)
	require.NoError(t, s.Put("old", []byte("v")))
	require.NoError(t, s.Put("recent", []byte("v")))
	setAge(t, s, "old", 48*time.Hour)
	setAge(t, s, "recent", time.Hour)

	s.Get("old")
	s.Get("recent")

	info, err := os.Stat(s.path("old"))
	require.NoError(t, err)
	require.WithinDuration(t, time.Now(), info.ModTime(), time.Minute)
	info, err = os.Stat(s.path("recent"))
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(-time.Hour), info.ModTime(), time.Minute, "no write within a day")
}

// Review focus 2: partial writes left by a crash are removed at startup and not counted.
func TestNewRemovesLeftoverTempFilesAndCountsEntries(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, func() int64 { return 1 << 20 }, util.NewLogger())
	require.NoError(t, err)
	require.NoError(t, s.Put("a", make([]byte, 30)))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "123.tmp"), make([]byte, 99), 0o644))

	reopened, err := New(dir, func() int64 { return 1 << 20 }, util.NewLogger())
	require.NoError(t, err)
	require.EqualValues(t, 30, reopened.Size())
	_, err = os.Stat(filepath.Join(dir, "123.tmp"))
	require.True(t, os.IsNotExist(err))
}

func TestClear(t *testing.T) {
	s, _ := newStore(t, 1<<20)
	require.NoError(t, s.Put("a", []byte("1")))
	require.NoError(t, s.Put("b", []byte("2")))

	require.NoError(t, s.Clear())
	require.EqualValues(t, 0, s.Size())
	_, ok := s.Get("a")
	require.False(t, ok)
}

func TestConcurrentUse(t *testing.T) {
	s, dir := newStore(t, 2000)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("k%d", i%10)
			// Errors are tolerated: on Windows, a rename over a file another goroutine
			// is reading can legitimately fail. We verify size accounting instead.
			_ = s.Put(key, make([]byte, 100))
			s.Get(key)
		}(i)
	}
	wg.Wait()
	require.LessOrEqual(t, s.Size(), int64(2000))

	// Verify size accounting is correct: s.Size() must equal the sum of entry file sizes on disk.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var diskTotal int64
	for _, e := range entries {
		if !e.IsDir() && !strings.HasSuffix(e.Name(), ".tmp") {
			info, err := e.Info()
			require.NoError(t, err)
			diskTotal += info.Size()
		}
	}
	require.Equal(t, diskTotal, s.Size(), "size accounting mismatch: Size() does not match disk contents")
}
