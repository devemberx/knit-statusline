package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

// write put lines in a temp transcript and return its path.
func write(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	var body string
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const assistantUsage = `{"type":"assistant","message":{"id":"msg_1","model":"claude-opus-5","usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`

func TestSessionStateMissingFileIsFresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.jsonl")
	if got := SessionState(path); got != StateFresh {
		t.Fatalf("SessionState = %v, want StateFresh", got)
	}
}

func TestSessionStateEmptyFileIsFresh(t *testing.T) {
	if got := SessionState(write(t)); got != StateFresh {
		t.Fatalf("SessionState = %v, want StateFresh", got)
	}
}

func TestSessionStateUserLinesOnlyAreFresh(t *testing.T) {
	path := write(t,
		`{"type":"user","message":{"role":"user","content":"hello"}}`,
		`{"type":"user","message":{"role":"user","content":"again"}}`,
	)
	if got := SessionState(path); got != StateFresh {
		t.Fatalf("SessionState = %v, want StateFresh", got)
	}
}

func TestSessionStateSyntheticOnlyIsFresh(t *testing.T) {
	path := write(t,
		`{"type":"assistant","message":{"id":"msg_s","model":"<synthetic>","usage":{"input_tokens":3,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
	)
	if got := SessionState(path); got != StateFresh {
		t.Fatalf("SessionState = %v, want StateFresh", got)
	}
}

func TestSessionStateAssistantUsageIsLive(t *testing.T) {
	if got := SessionState(write(t, assistantUsage)); got != StateLive {
		t.Fatalf("SessionState = %v, want StateLive", got)
	}
}

// Half-written or future-shaped line carrying "usage" cannot be ruled out.
// Fresh is claim that print zero, so absence of proof resolve live.
func TestSessionStateUndecodableUsageLineIsLive(t *testing.T) {
	path := write(t, `{"type":"assistant","message":{"usage":{"input_tokens":`)
	if got := SessionState(path); got != StateLive {
		t.Fatalf("SessionState = %v, want StateLive", got)
	}
}

func TestSessionStateEmptyPathIsLive(t *testing.T) {
	if got := SessionState(""); got != StateLive {
		t.Fatalf("SessionState = %v, want StateLive", got)
	}
}

func TestSessionStateUnreadableFileIsLive(t *testing.T) {
	path := write(t, assistantUsage)
	if err := os.Chmod(path, 0o000); err != nil {
		t.Skipf("chmod unsupported: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if os.Geteuid() == 0 {
		t.Skip("root read through mode 000")
	}
	if got := SessionState(path); got != StateLive {
		t.Fatalf("SessionState = %v, want StateLive", got)
	}
}

// Live transcript answer without reading to end.
func TestSessionStateStopsAtFirstUsageEntry(t *testing.T) {
	lines := []string{assistantUsage}
	for i := 0; i < 1000; i++ {
		lines = append(lines, `{"type":"user","message":{"role":"user","content":"filler"}}`)
	}
	if got := SessionState(write(t, lines...)); got != StateLive {
		t.Fatalf("SessionState = %v, want StateLive", got)
	}
}
