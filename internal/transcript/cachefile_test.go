package transcript

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// Calls that write or read whole file at once. Cache path is what they reach
// here, and every one of them join dir and name themselves -- empty dir then
// leave name relative, pointing at cwd of whichever process render under.
//
// os.Open stay off list: transcript itself is read that way, streaming, and it
// take path caller already resolved.
var cacheFileCalls = map[string]bool{
	"ReadFile":   true,
	"WriteFile":  true,
	"CreateTemp": true,
	"Rename":     true,
}

// Cache read and write live in cachefile.go alone, for its dir == "" guard.
//
// Rule live in that file's comment, and comment stopped nobody: PR #33 copied
// cursor.go's atomic write into new todo cursor, guard left behind. Review
// caught it because PR #32 had made empty root common days before.
func TestCacheIOStaysInCachefile(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	var scanned int
	for _, e := range entries {
		name := e.Name()
		switch {
		case !strings.HasSuffix(name, ".go"),
			strings.HasSuffix(name, "_test.go"),
			name == "cachefile.go":
			continue
		}

		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		scanned++

		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "os" || !cacheFileCalls[sel.Sel.Name] {
				return true
			}
			t.Errorf("%s:%d call os.%s; cache read and write go through cachefile.go",
				name, fset.Position(sel.Pos()).Line, sel.Sel.Name)
			return true
		})
	}

	// Miss here read as "package clean" while measuring nothing.
	if scanned == 0 {
		t.Fatal("scanned no package file")
	}
}
