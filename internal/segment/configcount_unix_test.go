//go:build unix

package segment

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

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
