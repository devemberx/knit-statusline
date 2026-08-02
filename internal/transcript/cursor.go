package transcript

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// Bump when aggregation rules change. Stale cache hold totals under old rules;
// mismatch discard rather than mix two definitions.
//
// 2: id-less entry stop clobbering dedup guard.
// 3: burned -- cursor carried ultracode marker state, reverted. Next bump take
// 4, else caches of that shape load as valid under new rules.
const cacheVersion = 2

// Cache hold per-file scan cursors for one scope.
type Cache struct {
	Version int                   `json:"version"`
	Files   map[string]FileCursor `json:"files"`
}

func NewCache() *Cache {
	return &Cache{Version: cacheVersion, Files: map[string]FileCursor{}}
}

// CacheKey name cache file per scope. Path hashed: name stay short and
// filesystem-safe whatever project layout.
func CacheKey(opts Options) string {
	h := sha256.Sum256([]byte(string(opts.Scope) + "\x00" + opts.TranscriptPath))
	return "tokens-" + hex.EncodeToString(h[:8]) + ".json"
}

// LoadCache read cache file. Missing, unreadable, corrupt or version-mismatched
// all yield empty cache: full rescan beat totals from unknown rules.
//
// Empty dir mean no config root at all. Join would drop directory element and
// read bare "tokens-<hash>.json" out of whatever directory process sit in.
func LoadCache(dir string, opts Options) *Cache {
	if dir == "" {
		return NewCache()
	}
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

// SaveCache write atomically. Claude Code start render while previous still
// run, so two processes write here at once. Unique temp file then rename =
// reader see one complete version or another, never half-written one.
//
// No fsync: this run every redraw, and cache lost to crash cost one rescan,
// same as LoadCache already do for corrupt content.
//
// Empty dir mean no config root, so nothing to write and nowhere to write it.
// Not error: caching optional, and MkdirAll("") fail every redraw over
// condition no user can fix.
func SaveCache(dir string, opts Options, c *Cache) error {
	if c == nil || dir == "" {
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
