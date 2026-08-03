package transcript

import (
	"os"
	"path/filepath"
)

// Cache file plumbing shared by every scan that persist a cursor. Each consumer
// keep its own key, version and payload -- token rules and todo rules move
// apart -- and share nothing but bytes on disk.
//
// Every cache in this package go through these two, never bare
// filepath.Join(dir, name) beside them. Guard below is one line each and both
// get lost to copy-paste: PR #33 hand-copied cursor.go's atomic write into new
// todo cursor and left dir == "" check behind, so cache loaded out of whatever
// project directory render happened to run in. cachefile_test.go enforce this;
// comment alone stop nobody.

// readCacheFile read dir/name, reporting whether bytes came back.
//
// Empty dir mean no config root at all. filepath.Join drop empty element, so
// unguarded read take bare "<name>" out of whatever directory process sit in --
// project checkout, where planted file feed counts nobody measured.
//
// Missing, unreadable and no-config-root answer alike: caller rescan cold, which
// beat totals from unknown rules.
func readCacheFile(dir, name string) ([]byte, bool) {
	if dir == "" {
		return nil, false
	}
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil, false
	}
	return b, true
}

// writeCacheFile put b at dir/name, replacing whatever sit there.
//
// Claude Code start render while previous still run, so two processes write here
// at once. Unique temp file then rename = reader see one complete version or
// another, never half-written one. Temp name derive from target, so two caches
// writing at once never collide on one pattern.
//
// No fsync: this run every redraw, and cache lost to crash cost one rescan, same
// as readCacheFile already do for corrupt content.
//
// Empty dir mean no config root, so nothing to write and nowhere to write it.
// Not error: caching optional, and MkdirAll("") fail every redraw over condition
// no user can fix.
func writeCacheFile(dir, name string, b []byte) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "."+name+".*.tmp")
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
	return os.Rename(tmpName, filepath.Join(dir, name))
}
