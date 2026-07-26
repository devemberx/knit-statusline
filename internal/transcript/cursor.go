package transcript

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// Bump whenever aggregation rules change. Stale cache hold totals under old
// rules; mismatch discard rather than mix two definitions.
const cacheVersion = 1

// Cache hold per-file scan cursors for one scope.
type Cache struct {
	Version int                   `json:"version"`
	Files   map[string]FileCursor `json:"files"`
}

func NewCache() *Cache {
	return &Cache{Version: cacheVersion, Files: map[string]FileCursor{}}
}

// CacheKey name cache file for a scope. Transcript path hashed: name stay short
// and filesystem-safe whatever a project layout.
func CacheKey(opts Options) string {
	h := sha256.Sum256([]byte(string(opts.Scope) + "\x00" + opts.TranscriptPath))
	return "tokens-" + hex.EncodeToString(h[:8]) + ".json"
}

// LoadCache read a cache file. Missing, unreadable, corrupt or version-mismatched
// all yield one empty cache: a full rescan beat totals from unknown rules.
func LoadCache(dir string, opts Options) *Cache {
	b, err := os.ReadFile(filepath.Join(dir, CacheKey(opts)))
	if err != nil {
		return NewCache()
	}
	var c Cache
	if err := json.Unmarshal(b, &c); err != nil || c.Version != cacheVersion {
		return NewCache()
	}
	if c.Files == nil {
		c.Files = map[string]FileCursor{}
	}
	return &c
}

// SaveCache write atomically.
//
// Claude Code start a render while a previous one still run, so two processes
// write here at once. Unique temp file then rename = reader see one complete
// version or another, never a half-written one.
func SaveCache(dir string, opts Options, c *Cache) error {
	if c == nil {
		return nil
	}
	c.Version = cacheVersion

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}

	final := filepath.Join(dir, CacheKey(opts))
	tmp, err := os.CreateTemp(dir, ".tokens-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, final)
}
