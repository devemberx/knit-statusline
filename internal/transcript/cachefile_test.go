package transcript

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// Calls a cache reader or writer reach for, each joining dir and name itself.
// Empty dir then leave name relative, pointing at cwd of whichever process
// render under.
//
// os.Open stay off list: transcript itself is read that way, streaming, from
// path caller already resolved. Writing openers stay on -- package hold no
// streaming write, so os.Create or os.OpenFile here mean cache bytes.
var cacheFileCalls = map[string]bool{
	"ReadFile":   true,
	"WriteFile":  true,
	"Create":     true,
	"CreateTemp": true,
	"OpenFile":   true,
	"Rename":     true,
}

// Cache read and write live in cachefile.go alone, for its dir == "" guard.
// Why comment alone never held that line: cachefile.go.
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
