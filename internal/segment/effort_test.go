package segment

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devemberx/knit-statusline/internal/config"
	"github.com/devemberx/knit-statusline/internal/render"
)

// Markers exactly as Claude Code 2.1.220 write them, verified 2026-08-01.
const (
	enterMarker = `{"type":"attachment","attachment":{"type":"ultra_effort_enter","reminderType":"full"}}`
	exitMarker  = `{"type":"attachment","attachment":{"type":"ultra_effort_exit"}}`
)

// effortDoc build minimal payload: effort level plus session transcript path.
func effortDoc(level, transcriptPath string) []byte {
	return []byte(fmt.Sprintf(`{"transcript_path":%q,"effort":{"level":%q}}`, transcriptPath, level))
}

// effortCtx mirror segment_test.go ctx, plus real cache dir -- effort now scan
// transcript when payload say xhigh.
func effortCtx(t *testing.T, doc []byte) Context {
	t.Helper()
	def, ok := Lookup("effort")
	if !ok {
		t.Fatal("effort segment not registered")
	}
	c := &config.Config{Segments: map[string]*config.Segment{}}
	return Context{
		In:       input(t, doc),
		Cfg:      c.Resolve("effort", def.DefaultTemplate),
		Palette:  render.NoColor(),
		Now:      time.Now(),
		CacheDir: t.TempDir(),
	}
}

func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	var b []byte
	for _, l := range lines {
		b = append(b, l...)
		b = append(b, '\n')
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEffortUltraFromTranscriptMarker(t *testing.T) {
	path := writeTranscript(t, enterMarker)
	if got := draw(effortCtx(t, effortDoc("xhigh", path))); got != "✺ ultra" {
		t.Errorf("rendered %q, want %q", got, "✺ ultra")
	}
}

func TestEffortXhighWithoutMarkerStaysPlain(t *testing.T) {
	path := writeTranscript(t)
	if got := draw(effortCtx(t, effortDoc("xhigh", path))); got != "● xhigh" {
		t.Errorf("rendered %q, want %q", got, "● xhigh")
	}
}

func TestEffortExitMarkerReverts(t *testing.T) {
	path := writeTranscript(t, enterMarker, exitMarker)
	if got := draw(effortCtx(t, effortDoc("xhigh", path))); got != "● xhigh" {
		t.Errorf("rendered %q, want %q", got, "● xhigh")
	}
}

// Enter marker may go stale: model switch drop ultracode with no exit marker
// (claude-code#80901). Payload gate bound that lie to xhigh sessions.
func TestEffortMarkerIgnoredOffXhigh(t *testing.T) {
	path := writeTranscript(t, enterMarker)
	if got := draw(effortCtx(t, effortDoc("high", path))); got != "◕ high" {
		t.Errorf("rendered %q, want %q", got, "◕ high")
	}
}

// claude-code#77812: some builds leak literal "ultracode" as effort.level.
// Same rendering, no transcript needed.
func TestEffortLiteralUltracodeLevel(t *testing.T) {
	if got := draw(effortCtx(t, effortDoc("ultracode", ""))); got != "✺ ultra" {
		t.Errorf("rendered %q, want %q", got, "✺ ultra")
	}
}

// Absent transcript degrade to plain xhigh, never blank, never error.
func TestEffortMissingTranscriptTolerated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.jsonl")
	if got := draw(effortCtx(t, effortDoc("xhigh", path))); got != "● xhigh" {
		t.Errorf("rendered %q, want %q", got, "● xhigh")
	}
}
