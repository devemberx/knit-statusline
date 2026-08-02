package transcript

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// LoadTodoCursor read cache file. Missing, unreadable, corrupt,
// version-mismatched or no config root at all yield zero cursor: full rescan
// beat counts from unknown rules.
func LoadTodoCursor(dir, transcriptPath string) TodoCursor {
	b, ok := readCacheFile(dir, TodoCacheKey(transcriptPath))
	if !ok {
		return TodoCursor{}
	}
	var c todoCache
	if err := json.Unmarshal(b, &c); err != nil || c.Version != todoCacheVersion {
		return TodoCursor{}
	}
	return TodoCursor{Offset: c.Offset, Todos: c.Todos}
}

// SaveTodoCursor write atomically -- see writeCacheFile for why temp then
// rename, and why empty dir answer nil.
func SaveTodoCursor(dir, transcriptPath string, cur TodoCursor) error {
	b, err := json.Marshal(todoCache{
		Version: todoCacheVersion,
		Offset:  cur.Offset,
		Todos:   cur.Todos,
	})
	if err != nil {
		return err
	}
	return writeCacheFile(dir, TodoCacheKey(transcriptPath), b)
}
