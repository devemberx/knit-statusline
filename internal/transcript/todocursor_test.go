package transcript

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTodoCursorRoundTrips(t *testing.T) {
	dir := t.TempDir()
	want := TodoCursor{Offset: 512, Todos: Todos{Done: 3, Total: 7}}

	if err := SaveTodoCursor(dir, "/tmp/s.jsonl", want); err != nil {
		t.Fatalf("SaveTodoCursor: %v", err)
	}
	if got := LoadTodoCursor(dir, "/tmp/s.jsonl"); got != want {
		t.Errorf("LoadTodoCursor = %+v, want %+v", got, want)
	}
}

// Two transcripts must not share one record, else switching sessions read
// other one's offset into this one's file.
func TestTodoCacheKeyVariesByPath(t *testing.T) {
	if TodoCacheKey("/a.jsonl") == TodoCacheKey("/b.jsonl") {
		t.Error("two transcripts hashed to one cache file")
	}
	if !strings.HasPrefix(TodoCacheKey("/a.jsonl"), "todos-") {
		t.Errorf("cache key %q lost its prefix", TodoCacheKey("/a.jsonl"))
	}
	// tokens cache live in same directory. Colliding names mean one decode
	// other's JSON and rescan from a meaningless offset.
	if TodoCacheKey("/a.jsonl") == CacheKey(Options{TranscriptPath: "/a.jsonl", Scope: ScopeSession}) {
		t.Error("todo cache key collide with tokens cache key")
	}
}

// Empty dir mean no config root resolved. filepath.Join drop empty element, so
// unguarded LoadTodoCursor read bare "todos-<hash>.json" out of whatever
// directory process sit in. Unguarded SaveTodoCursor never reach that name --
// MkdirAll("") fail first -- but it fail every redraw over condition no user can
// fix. Same guard tokens cache carry.
func TestTodoCursorWithoutDirectoryStaysOutOfWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Decoy carry counts no scan produced, plus offset that skip real lines.
	// Loading it report todo list this session never wrote.
	b, err := json.Marshal(todoCache{
		Version: todoCacheVersion,
		Offset:  999,
		Todos:   Todos{Done: 9, Total: 9},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(TodoCacheKey("/x/y.jsonl"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	if got := LoadTodoCursor("", "/x/y.jsonl"); got != (TodoCursor{}) {
		t.Errorf(`LoadTodoCursor("") read working directory: %+v`, got)
	}
	if err := SaveTodoCursor("", "/x/y.jsonl", TodoCursor{Offset: 1}); err != nil {
		t.Errorf(`SaveTodoCursor("") = %v, want nil`, err)
	}

	// Decoy byte-for-byte after. SaveTodoCursor that tolerate "" by skipping
	// MkdirAll rename over this exact name instead.
	after, err := os.ReadFile(TodoCacheKey("/x/y.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, b) {
		t.Errorf(`SaveTodoCursor("") rewrote %s in the working directory`, TodoCacheKey("/x/y.jsonl"))
	}
}

// Missing file mean cold-start case, not error.
func TestLoadTodoCursorMissingFileIsZero(t *testing.T) {
	if got := LoadTodoCursor(t.TempDir(), "/tmp/s.jsonl"); got != (TodoCursor{}) {
		t.Errorf("missing cache gave %+v, want zero", got)
	}
}

// Corrupt content and a version from other rules both mean rescan cold: totals
// from unknown rules beat nothing only if you trust rules.
func TestLoadTodoCursorDiscardsUnusableFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"corrupt", "{not json"},
		{"version mismatch", `{"version":999,"offset":10,"todos":{"done":1,"total":2}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, TodoCacheKey("/tmp/s.jsonl"))
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := LoadTodoCursor(dir, "/tmp/s.jsonl"); got != (TodoCursor{}) {
				t.Errorf("loaded %+v from %s cache, want zero", got, tc.name)
			}
		})
	}
}

// Version stamped on write, so a cache written today load tomorrow.
func TestSaveTodoCursorStampsVersion(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTodoCursor(dir, "/tmp/s.jsonl", TodoCursor{Offset: 1}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, TodoCacheKey("/tmp/s.jsonl")))
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Version != todoCacheVersion {
		t.Errorf("version = %d, want %d", raw.Version, todoCacheVersion)
	}
}

// Two renders overlap. Write must leave one whole version, old or new,
// never a half-written file, and never a temp file behind.
func TestSaveTodoCursorLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTodoCursor(dir, "/tmp/s.jsonl", TodoCursor{Offset: 1}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("cache dir hold %v, want one file", names)
	}
}

// Cache dir may not exist on a first render.
func TestSaveTodoCursorCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "cache")
	if err := SaveTodoCursor(dir, "/tmp/s.jsonl", TodoCursor{Offset: 1}); err != nil {
		t.Fatalf("SaveTodoCursor: %v", err)
	}
	if got := LoadTodoCursor(dir, "/tmp/s.jsonl"); got.Offset != 1 {
		t.Errorf("offset = %d, want 1", got.Offset)
	}
}
