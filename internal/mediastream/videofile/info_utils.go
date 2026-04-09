package videofile

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"strings"
)

func GetHashFromPath(path string) (string, error) {
	// For URLs, hash the URL string directly (no os.Stat)
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return GetHashFromURL(path), nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	h := sha1.New()
	h.Write([]byte(path))
	h.Write([]byte(info.ModTime().String()))
	sha := hex.EncodeToString(h.Sum(nil))
	return sha, nil
}

// GetHashFromURL generates a hash from a URL string.
func GetHashFromURL(url string) string {
	h := sha1.New()
	h.Write([]byte(url))
	return hex.EncodeToString(h.Sum(nil))
}
