//go:build unix

package segment

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// FIFO renamed over flag between Lstat and Open hold os.Open until some writer
// arrive, and render path carry no timeout to cut that. Handing readIfSame a
// FIFO under regular file's FileInfo stand in for that window.
func TestCavemanRefusesFifoSwappedAfterStat(t *testing.T) {
	dir := t.TempDir()
	flag := filepath.Join(dir, ".caveman-active")
	if err := syscall.Mkfifo(flag, 0o600); err != nil {
		t.Skipf("mkfifo unsupported here: %v", err)
	}
	regular := filepath.Join(dir, "regular")
	if err := os.WriteFile(regular, []byte("full"), 0o644); err != nil {
		t.Fatalf("write regular: %v", err)
	}
	fi, err := os.Lstat(regular)
	if err != nil {
		t.Fatalf("lstat regular: %v", err)
	}

	done := make(chan string, 1)
	go func() {
		got, ok := readIfSame(flag, fi)
		if ok {
			done <- got
			return
		}
		done <- ""
	}()

	select {
	case got := <-done:
		if got != "" {
			t.Errorf("read FIFO under regular file's stat and got %q, want refusal", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("readIfSame blocked on FIFO flag")
	}
}
