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
// Handing readRegular a FIFO stand in for that window, microseconds wide and
// never lost on purpose.
func TestConfigRefusesFifoSwappedAfterStat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo unsupported here: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := readRegular(path, maxConfigBytes)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, errConfigNotRegular) {
			t.Errorf("read FIFO gave %v, want errConfigNotRegular", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("readRegular blocked on FIFO settings.json")
	}
}

// settings.json as FIFO block os.Open until some writer show up, and render
// path carry no timeout to cut that. Blocked render print nothing at all, which
// read as crash.
func TestConfigRefusesFifoSettings(t *testing.T) {
	c := configCtx(t)
	if err := syscall.Mkfifo(filepath.Join(c.ConfigDir, "settings.json"), 0o600); err != nil {
		t.Skipf("mkfifo unsupported here: %v", err)
	}

	done := make(chan string, 1)
	go func() { done <- draw(c) }()

	select {
	case got := <-done:
		if want := "🪝… · 🔌…"; got != want {
			t.Errorf("rendered %q, want %q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("render blocked on FIFO settings.json")
	}
}
