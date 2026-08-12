//go:build unix

package segment

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Stat and open are two syscalls. FIFO renamed over path in between pass Stat
// as regular file it replace, and open then wait for writer that never come.
// FIFO handed straight to readRegular stand in for that window, microseconds
// wide and never lost on purpose.
func TestReadRegularRefusesFifoSwappedAfterStat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo unsupported here: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := readRegular(path, maxClaudeJSONBytes)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, errConfigNotRegular) {
			t.Errorf("read FIFO gave %v, want errConfigNotRegular", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("readRegular blocked on FIFO .claude.json")
	}
}
