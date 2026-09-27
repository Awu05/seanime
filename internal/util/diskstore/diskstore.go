// Package diskstore keeps one file per entry in a directory and trims the least recently used
// entries once the total size passes a limit.
package diskstore

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// touchInterval bounds how often a read refreshes an entry's age, so busy pages don't turn every
// view into a disk write.
const touchInterval = 24 * time.Hour

const tempSuffix = ".tmp"

type Store struct {
	dir      string
	maxBytes func() int64
	logger   *zerolog.Logger

	mu    sync.Mutex // guards total and serializes writes, deletes and trimming
	total int64
}

// New opens the store at dir, creating it if needed. maxBytes is read on every trim, so a new
// limit applies without reopening. Partial writes left by a crash are removed.
func New(dir string, maxBytes func() int64, logger *zerolog.Logger) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, maxBytes: maxBytes, logger: logger}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), tempSuffix) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
			continue
		}
		if info, err := e.Info(); err == nil {
			s.total += info.Size()
		}
	}
	return s, nil
}

// path hashes the key so any string (e.g. a long URL) maps to a short, safe file name.
func (s *Store) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:]))
}

// Get returns the entry for key, or false if there is none or it can't currently be read. A read
// error is treated as a miss rather than a deletion, since it may just be transient (e.g. a
// concurrent write on Windows); trimming clears out anything genuinely bad over time.
func (s *Store) Get(key string) ([]byte, bool) {
	p := s.path(key)
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	if info, err := os.Stat(p); err == nil && time.Since(info.ModTime()) > touchInterval {
		now := time.Now()
		_ = os.Chtimes(p, now, now)
	}
	return data, true
}

// Put saves data under key. It writes a temp file and renames it into place, so a partial write
// is never read back, then trims if the store is over its limit.
func (s *Store) Put(key string, data []byte) error {
	tmp, err := os.CreateTemp(s.dir, "*"+tempSuffix)
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.path(key)
	var replaced int64
	if info, err := os.Stat(p); err == nil {
		replaced = info.Size()
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	s.total += int64(len(data)) - replaced
	s.trimLocked()
	return nil
}

func (s *Store) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.path(key)
	info, err := os.Stat(p)
	if err != nil {
		return
	}
	if os.Remove(p) == nil {
		s.total -= info.Size()
	}
}

// Trim deletes the least recently used entries if the store is over its limit.
func (s *Store) Trim() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trimLocked()
}

func (s *Store) trimLocked() {
	limit := s.maxBytes()
	if s.total <= limit {
		return
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		s.logger.Warn().Err(err).Msg("diskstore: Could not list entries to trim")
		return
	}
	type entry struct {
		path    string
		size    int64
		modTime time.Time
	}
	files := make([]entry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), tempSuffix) {
			continue
		}
		if info, err := e.Info(); err == nil {
			files = append(files, entry{filepath.Join(s.dir, e.Name()), info.Size(), info.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.Before(files[j].modTime) })

	target := limit * 9 / 10
	for _, f := range files {
		if s.total <= target {
			break
		}
		if os.Remove(f.path) == nil {
			s.total -= f.size
		}
	}
}

func (s *Store) Size() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.total
}

// Clear deletes every entry. If it returns early on an error, total still matches what's left on disk.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, infoErr := e.Info()
		if err := os.Remove(filepath.Join(s.dir, e.Name())); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
		} else if infoErr == nil {
			s.total -= info.Size()
		}
	}
	s.total = 0 // every entry is gone, regardless of any Info() misses above
	return nil
}
