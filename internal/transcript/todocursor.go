package transcript

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// Bump when todo counting rules change. Own number, not cacheVersion: token
// rules and todo rules move apart, and one shared number retire caches that are
// still correct.
const todoCacheVersion = 1

// todoCache is one session's record. Single scope, so no per-file map: todo list
// belong to one transcript, and summing sibling ones report a list nobody kept.
type todoCache struct {
	Version int   `json:"version"`
	Offset  int64 `json:"offset"`
	Todos   Todos `json:"todos"`
}

// TodoCacheKey name cache file per transcript. Path hashed: name stay short and
// filesystem-safe whatever project layout. Prefix differ from CacheKey's, so
// two never land on one file in a shared directory.
func TodoCacheKey(transcriptPath string) string {
	h := sha256.Sum256([]byte(transcriptPath))
	return "todos-" + hex.EncodeToString(h[:8]) + ".json"
}

// LoadTodoCursor read cache file. Missing, unreadable, corrupt or
// version-mismatched all yield zero cursor: full rescan beat counts from
// unknown rules.
func LoadTodoCursor(dir, transcriptPath string) TodoCursor {
	b, err := os.ReadFile(filepath.Join(dir, TodoCacheKey(transcriptPath)))
	if err != nil {
		return TodoCursor{}
	}
	var c todoCache
	if err := json.Unmarshal(b, &c); err != nil || c.Version != todoCacheVersion {
		return TodoCursor{}
	}
	return TodoCursor{Offset: c.Offset, Todos: c.Todos}
}

// SaveTodoCursor write atomically. Claude Code start render while previous still
// run, so two processes write here at once. Unique temp file then rename =
// reader see one complete version or another, never half-written one.
//
// No fsync: this run every redraw, and cache lost to crash cost one rescan,
// same as LoadTodoCursor already do for corrupt content.
func SaveTodoCursor(dir, transcriptPath string, cur TodoCursor) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(todoCache{
		Version: todoCacheVersion,
		Offset:  cur.Offset,
		Todos:   cur.Todos,
	})
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".todos-*.tmp")
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
	return os.Rename(tmpName, filepath.Join(dir, TodoCacheKey(transcriptPath)))
}
