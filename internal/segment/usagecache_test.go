package segment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devemberx/knit-statusline/internal/fixtures"
)

// movedRoot build layout CLAUDE_CONFIG_DIR leave: config root one directory
// under home, and .claude.json sitting in both.
func movedRoot(t *testing.T) (home, root string) {
	t.Helper()
	home = t.TempDir()
	root = filepath.Join(home, ".claude")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	return home, root
}

// Half-written file catch render mid-rewrite, and it is first path probe. Live
// cache sit one directory up: giving up here drop both limit segment, neither
// Stable, so row lose slot with no marker and doctor report nothing.
func TestUsageCacheWalksPastUnreadableFirstFile(t *testing.T) {
	home, root := movedRoot(t)
	if err := os.WriteFile(filepath.Join(root, ".claude.json"), []byte(`{"cachedUsageUtil`), 0o644); err != nil {
		t.Fatalf("write inner .claude.json: %v", err)
	}
	writeUsageCache(t, home, usageFetchedAt,
		scopedLimitJSON("weekly_scoped", "42", modelScope("Opus"), "2025-07-28T20:59:59.810071+00:00"),
	)

	res := Build(modelLimitCtx(t, fixtures.Full, root, time.Minute))
	if res.Empty {
		t.Fatal("broken first file hid usage block one directory up")
	}
	if got := res.Fields["pct"].Text; got != "42" {
		t.Errorf("pct = %q, want %q", got, "42")
	}
}

// Same walk for file refused on size rather than parse: stale copy left inside
// moved root run past cap while live one beside it read fine.
func TestUsageCacheWalksPastOversizeFirstFile(t *testing.T) {
	home, root := movedRoot(t)
	pad := strings.Repeat("x", maxClaudeJSONBytes)
	if err := os.WriteFile(filepath.Join(root, ".claude.json"), []byte(`{"pad":"`+pad+`"}`), 0o644); err != nil {
		t.Fatalf("write oversize .claude.json: %v", err)
	}
	writeUsageCache(t, home, usageFetchedAt,
		scopedLimitJSON("weekly_scoped", "42", modelScope("Opus"), "2025-07-28T20:59:59.810071+00:00"),
	)

	if res := Build(modelLimitCtx(t, fixtures.Full, root, time.Minute)); res.Empty {
		t.Error("oversize first file hid usage block one directory up")
	}
}

// Both file unreadable prove nothing about window, so row drop rather than
// print number no render measured.
func TestUsageCacheDropsWhenEveryPathUnreadable(t *testing.T) {
	home, root := movedRoot(t)
	for _, dir := range []string{root, home} {
		if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"cachedUsageUtil`), 0o644); err != nil {
			t.Fatalf("write %s/.claude.json: %v", dir, err)
		}
	}

	if res := Build(modelLimitCtx(t, fixtures.Full, root, time.Minute)); !res.Empty {
		t.Errorf("unreadable cache rendered %+v, want empty", res.Fields)
	}
}

// limit.model and limit.extra read one block off one file up to 4 MiB, and
// statusline memo key on segment name, so it dedupe neither. Deleting file
// between two Build stand in for read count: second segment answering off memo
// never touch disk again.
func TestUsageMemoReadsCacheOncePerRender(t *testing.T) {
	root := t.TempDir()
	writeExtraUsage(t, root, usageFetchedAt, `{"is_enabled": true, "utilization": 18}`)

	c := extraCtx(t, root, time.Minute)
	c.Usage = &UsageMemo{}
	if res := Build(c); res.Empty {
		t.Fatal("first read dropped segment")
	}

	if err := os.Remove(filepath.Join(root, ".claude.json")); err != nil {
		t.Fatalf("remove .claude.json: %v", err)
	}
	if res := Build(c); res.Empty {
		t.Error("second segment re-read disk, want block off memo")
	}
}

// Nil memo is what test and any caller building Context by hand hand over.
// Read run per call there rather than panic.
func TestUsageMemoNilReadsPerCall(t *testing.T) {
	root := t.TempDir()
	writeExtraUsage(t, root, usageFetchedAt, `{"is_enabled": true, "utilization": 18}`)

	c := extraCtx(t, root, time.Minute)
	if c.Usage != nil {
		t.Fatal("extraCtx handed memo, test measure nil path")
	}
	if res := Build(c); res.Empty {
		t.Fatal("nil memo dropped segment")
	}

	if err := os.Remove(filepath.Join(root, ".claude.json")); err != nil {
		t.Fatalf("remove .claude.json: %v", err)
	}
	if res := Build(c); !res.Empty {
		t.Errorf("nil memo answered off cache: %+v", res.Fields)
	}
}
