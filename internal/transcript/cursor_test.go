package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	writeLines(t, path, []string{assistantLine("msg_a", "claude-opus-4-8", 100, 10, 1000, 5)})

	opts := Options{TranscriptPath: path, Scope: ScopeSession}
	_, cache := Scan(opts, nil)

	cacheDir := filepath.Join(dir, "cache")
	if err := SaveCache(cacheDir, opts, cache); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}

	loaded := LoadCache(cacheDir, opts)
	if loaded.Files[path] != cache.Files[path] {
		t.Errorf("round trip lost state: %+v vs %+v", loaded.Files[path], cache.Files[path])
	}

	// No temp file survive one atomic write.
	entries, _ := os.ReadDir(cacheDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}

func TestLoadCacheToleratesGarbage(t *testing.T) {
	dir := t.TempDir()
	opts := Options{TranscriptPath: "/x/y.jsonl", Scope: ScopeSession}

	if err := os.WriteFile(filepath.Join(dir, CacheKey(opts)), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := LoadCache(dir, opts)
	if c == nil || len(c.Files) != 0 || c.Version != cacheVersion {
		t.Errorf("corrupt cache should yield a fresh one, got %+v", c)
	}
}

// Whole point of cacheVersion: totals written under old aggregation rules get
// dropped, never summed against totals written under new ones.
func TestLoadCacheDiscardsVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	opts := Options{TranscriptPath: "/x/y.jsonl", Scope: ScopeSession}

	stale, err := json.Marshal(Cache{
		Version: cacheVersion - 1,
		Files:   map[string]FileCursor{"/x/y.jsonl": {Offset: 10, Totals: Totals{Input: 999}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, CacheKey(opts)), stale, 0o644); err != nil {
		t.Fatal(err)
	}

	c := LoadCache(dir, opts)
	if len(c.Files) != 0 {
		t.Errorf("stale-version cursors survived: %+v", c.Files)
	}
	if c.Version != cacheVersion {
		t.Errorf("version = %d, want %d", c.Version, cacheVersion)
	}
}

// Scope feed key hash. Dropped from it, session and project share one cache
// file and each report other's totals.
func TestCacheKeyVariesByScope(t *testing.T) {
	opts := Options{TranscriptPath: "/x/y.jsonl"}
	opts.Scope = ScopeSession
	session := CacheKey(opts)
	opts.Scope = ScopeProject
	project := CacheKey(opts)

	if session == project {
		t.Errorf("session and project share cache key %s", session)
	}
	for _, k := range []string{session, project} {
		if filepath.Base(k) != k {
			t.Errorf("cache key %q is not a bare filename", k)
		}
	}
}

// Scan hand back nil cache on no path. Saving it must not create file that
// LoadCache then treat as empty scan.
func TestSaveCacheNilIsNoop(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	opts := Options{TranscriptPath: "/x/y.jsonl", Scope: ScopeSession}

	if err := SaveCache(dir, opts, nil); err != nil {
		t.Fatalf("SaveCache(nil): %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("nil cache created %s", dir)
	}
}
