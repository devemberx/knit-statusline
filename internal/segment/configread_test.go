package segment

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Cap is only thing between render path and .claude.json that grew past what
// parse cost bound -- render carry no timeout to cut it. Coverage lived in two
// config-segment tests deleted with that segment, so it get stated here.
func TestReadConfigJSONRefusesFileOverCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	pad := strings.Repeat("x", maxClaudeJSONBytes)
	if err := os.WriteFile(path, []byte(`{"pad":"`+pad+`"}`), 0o644); err != nil {
		t.Fatalf("write oversize file: %v", err)
	}

	var v map[string]any
	if err := readConfigJSON(path, maxClaudeJSONBytes, &v); !errors.Is(err, errConfigTooBig) {
		t.Errorf("read gave %v, want errConfigTooBig", err)
	}
}

// Guard cut oversize file alone: one right under cap still decode, else every
// large-but-legal .claude.json read as absent.
func TestReadConfigJSONReadsFileUnderCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	doc := `{"pad":"` + strings.Repeat("x", maxClaudeJSONBytes-64) + `"}`
	if int64(len(doc)) > maxClaudeJSONBytes {
		t.Fatalf("fixture %d bytes, over cap %d", len(doc), maxClaudeJSONBytes)
	}
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatalf("write under-cap file: %v", err)
	}

	var v map[string]string
	if err := readConfigJSON(path, maxClaudeJSONBytes, &v); err != nil {
		t.Fatalf("read under-cap file: %v", err)
	}
	if got := len(v["pad"]); got != maxClaudeJSONBytes-64 {
		t.Errorf("pad read %d bytes, want %d", got, maxClaudeJSONBytes-64)
	}
}

// Cap edge itself: one byte over refused, file sitting exactly on limit read.
func TestReadCappedFileHoldsLimitEdge(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 65)), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if _, err := readCappedFile(path, 64); !errors.Is(err, errConfigTooBig) {
		t.Errorf("read gave %v, want errConfigTooBig", err)
	}
	if _, err := readCappedFile(path, 65); err != nil {
		t.Errorf("file exactly at limit gave %v, want read", err)
	}
}
