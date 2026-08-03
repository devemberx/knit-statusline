package transcript

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Bump when aggregation rules change. Stale cache hold totals under old rules;
// mismatch discard rather than mix two definitions.
//
// 2: id-less entry stop clobbering dedup guard.
// 3: burned -- cursor carried ultracode marker state, reverted. Next bump take
// 4, else caches of that shape load as valid under new rules.
// 4: cursor carry skill listing count and last Skill invocation.
const cacheVersion = 4

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
//
// "tokens-" prefix predate skill state riding in same cursor. Renaming orphan
// every cache on disk for a filename nobody read, so prefix stay.
func CacheKey(opts Options) string {
	h := sha256.Sum256([]byte(string(opts.Scope) + "\x00" + opts.TranscriptPath))
	return "tokens-" + hex.EncodeToString(h[:8]) + ".json"
}

// LoadCache read cache file. Missing, unreadable, corrupt, version-mismatched or
// no config root at all yield empty cache: full rescan beat totals from unknown
// rules.
func LoadCache(dir string, opts Options) *Cache {
	b, ok := readCacheFile(dir, CacheKey(opts))
	if !ok {
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

// SaveCache write atomically -- see writeCacheFile for why temp then rename, and
// why empty dir answer nil.
//
// Nil cache mean nothing scanned, so nothing to persist. Writing empty one
// create cache directory no render ever used.
func SaveCache(dir string, opts Options, c *Cache) error {
	if c == nil {
		return nil
	}
	c.Version = cacheVersion

	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return writeCacheFile(dir, CacheKey(opts), b)
}
