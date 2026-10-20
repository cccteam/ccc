package skeleton

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/mod/modfile"
)

const modulePrefix = "github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/"

func TestCandidates(t *testing.T) {
	t.Parallel()

	got, err := Candidates()
	if err != nil {
		t.Fatalf("Candidates() error = %v", err)
	}

	want := []Candidate{
		{Name: "outlets", ModulePath: modulePrefix + "outlets"},
		{Name: "sites", ModulePath: modulePrefix + "sites"},
		{Name: "solo", ModulePath: modulePrefix + "solo"},
		{Name: "tenanted", ModulePath: modulePrefix + "tenanted"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Candidates() mismatch (-want +got):\n%s", diff)
	}
}

func TestFS(t *testing.T) {
	t.Parallel()

	// A template is a complete application minus its build products: these files must
	// be present in every candidate, and nothing from these directories may be embedded.
	required := []string{ModFile, "go.sum", ".gitignore", ".golangci.yml", ".envrc.template", "Procfile", "README.md", "pkg/auth/staff/roles.json", "pkg/auth/staff/staff.go", ".github/workflows/ci.yml", ".github/workflows/ci-cache.yml", ".github/workflows/security-scan.yml", "schema/firestore/firestore.rules", "schema/firestore/firestore.indexes.json"}
	forbiddenDirs := []string{"node_modules", ".angular", "dist", ".ccc-cache", ".yalc", ".git"}
	// codeql-config.yml left with the CodeQL job: nothing reads it.
	forbiddenFiles := []string{"go.mod", "go.work", "go.work.sum", ".envrc", ".overmind.sock", "yalc.lock", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "codeql-config.yml"}

	tests := []struct {
		name    string
		wantErr bool
		// entry is the file that proves the layout: main.go at the root for a
		// flat template, the apps directory for the sites layout. lockfile is the bun
		// lockfile of the (first) browser workspace; bunfig.toml sits beside it.
		entry, lockfile string
	}{
		{name: "solo", entry: "main.go", lockfile: "web/bun.lock"},
		{name: "tenanted", entry: "main.go", lockfile: "web/bun.lock"},
		{name: "outlets", entry: "main.go", lockfile: "web/bun.lock"},
		{name: "sites", entry: "apps/console/main.go", lockfile: "apps/console/web/bun.lock"},
		{name: "unknown", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sub, err := FS(tt.name)
			if (err != nil) != tt.wantErr {
				t.Fatalf("FS() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			for _, rel := range append(required, tt.entry, tt.lockfile, path.Join(path.Dir(tt.lockfile), "bunfig.toml")) {
				if _, err := fs.Stat(sub, rel); err != nil {
					t.Errorf("%s: missing %s: %v", tt.name, rel, err)
				}
			}

			err = fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				for _, dir := range forbiddenDirs {
					if d.IsDir() && d.Name() == dir {
						t.Errorf("%s: build product embedded at %s", tt.name, p)

						return fs.SkipDir
					}
				}
				for _, file := range forbiddenFiles {
					if !d.IsDir() && d.Name() == file {
						t.Errorf("%s: %s embedded at %s", tt.name, file, p)
					}
				}
				if !d.IsDir() && path.Ext(p) == ".go" {
					if err := checkImports(sub, p, tt.name); err != nil {
						t.Error(err)
					}
				}

				return nil
			})
			if err != nil {
				t.Fatalf("fs.WalkDir() error = %v", err)
			}
		})
	}
}

// TestModFileCarriesTheImpulseTool pins the impulse pin's home: every candidate's go.mod
// carries the tool directive for impulse and a require of it, so a rendered application
// runs go tool impulse check at a version go.mod names and Go's checksum database verifies.
func TestModFileCarriesTheImpulseTool(t *testing.T) {
	t.Parallel()

	const impulseModule = "github.com/cccteam/ccc/impulse"
	tests := []struct {
		name string
	}{
		{name: "solo"}, {name: "tenanted"}, {name: "outlets"}, {name: "sites"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sub, err := FS(tt.name)
			if err != nil {
				t.Fatalf("FS() error = %v", err)
			}
			data, err := fs.ReadFile(sub, ModFile)
			if err != nil {
				t.Fatalf("fs.ReadFile(%s) error = %v", ModFile, err)
			}
			mod, err := modfile.Parse(ModFile, data, nil)
			if err != nil {
				t.Fatalf("modfile.Parse() error = %v", err)
			}
			held := false
			for _, tool := range mod.Tool {
				if tool.Path == impulseModule {
					held = true
				}
			}
			if !held {
				t.Errorf("%s: no tool directive for %s", tt.name, impulseModule)
			}
			pinned := ""
			for _, r := range mod.Require {
				if r.Mod.Path == impulseModule {
					pinned = r.Mod.Version
				}
			}
			if pinned == "" {
				t.Errorf("%s: no require of %s", tt.name, impulseModule)
			}
		})
	}
}

// checkImports fails when a Go file still imports the template by any path other than
// its own placeholder module path, which rendering would then fail to rewrite.
func checkImports(sub fs.FS, p, name string) error {
	src, err := fs.ReadFile(sub, p)
	if err != nil {
		return errors.Wrap(err, "fs.ReadFile()")
	}
	for _, line := range strings.Split(string(src), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "cccteam/impulse") && !strings.Contains(line, "cccteam/ccc/impulse") {
			continue
		}
		if !strings.Contains(line, modulePrefix+name) {
			return &importError{candidate: name, file: p, line: line}
		}
	}

	return nil
}

type importError struct {
	candidate, file, line string
}

func (e *importError) Error() string {
	return e.candidate + ": " + e.file + " imports the template by a stale path: " + e.line
}

// TestDataLevelConstructor pins the data level's shape: every auth the level holds
// (a *<auth>.Auth field of DataConfiguration) has its settings built by a method of the
// environment struct, <auth>Settings, which NewDataConfiguration calls once in place of
// a Settings literal, so the constructor stays under the lint configuration's funlen
// limit as registrations accumulate; impulse add method extends the method, and
// impulse add auth copies it.
func TestDataLevelConstructor(t *testing.T) {
	t.Parallel()

	const (
		dataFile    = "pkg/config/data.go"
		constructor = "NewDataConfiguration"
		envStruct   = "dataConfig"
	)
	tests := []struct {
		name string
		// auths are the auths the candidate's data level constructs.
		auths []string
	}{
		{name: "solo", auths: []string{"staff"}},
		{name: "tenanted", auths: []string{"staff"}},
		{name: "outlets", auths: []string{"staff", "members"}},
		{name: "sites", auths: []string{"staff"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sub, err := FS(tt.name)
			if err != nil {
				t.Fatalf("FS() error = %v", err)
			}
			src, err := fs.ReadFile(sub, dataFile)
			if err != nil {
				t.Fatalf("fs.ReadFile(%s) error = %v", dataFile, err)
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, dataFile, src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parser.ParseFile() error = %v", err)
			}
			limit := funlenLimit(t, sub)
			decls := map[string]*ast.FuncDecl{}
			for _, d := range file.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok {
					decls[funcKey(fd)] = fd
				}
			}
			fd := decls[constructor]
			if fd == nil {
				t.Fatalf("%s declares no %s", dataFile, constructor)
			}
			if lines := fset.Position(fd.End()).Line - fset.Position(fd.Pos()).Line + 1; lines > limit {
				t.Errorf("%s is %d lines, over the lint configuration's funlen limit of %d", constructor, lines, limit)
			}
			if got := authFields(file); !cmp.Equal(tt.auths, got) {
				t.Fatalf("DataConfiguration holds the auths %v, want %v", got, tt.auths)
			}
			for _, auth := range tt.auths {
				method := auth + "Settings"
				settings, ok := decls[envStruct+"."+method]
				if !ok {
					t.Errorf("%s declares no %s method on %s", dataFile, method, envStruct)

					continue
				}
				if want := "*" + auth + ".Settings"; settings.Type.Results == nil || len(settings.Type.Results.List) != 1 || exprString(settings.Type.Results.List[0].Type) != want {
					t.Errorf("%s.%s does not return %s", envStruct, method, want)
				}
				if n := countCalls(fd.Body, "env."+method); n != 1 {
					t.Errorf("%s calls env.%s %d time(s), want once", constructor, method, n)
				}
				if countLiterals(fd.Body, auth+".Settings") != 0 {
					t.Errorf("%s builds a %s.Settings literal inline; the literal belongs in %s", constructor, auth, method)
				}
			}
		})
	}
}

// funlenLimit reads the funlen lines limit the candidate's lint configuration sets.
func funlenLimit(t *testing.T, sub fs.FS) int {
	t.Helper()

	data, err := fs.ReadFile(sub, lintConfig)
	if err != nil {
		t.Fatalf("fs.ReadFile(%s) error = %v", lintConfig, err)
	}
	m := regexp.MustCompile(`(?m)^\s*funlen:\n\s*lines: (\d+)$`).FindSubmatch(data)
	if m == nil {
		t.Fatalf("%s sets no funlen lines limit", lintConfig)
	}
	limit, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatal(err)
	}

	return limit
}

// funcKey names a function, or a method as <Type>.<name>.
func funcKey(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) != 1 {
		return fd.Name.Name
	}
	recv := fd.Recv.List[0].Type
	if star, ok := recv.(*ast.StarExpr); ok {
		recv = star.X
	}

	return exprString(recv) + "." + fd.Name.Name
}

// authFields lists the auths DataConfiguration holds, in field order: each field whose
// type is *<auth>.Auth.
func authFields(file *ast.File) []string {
	var auths []string
	ast.Inspect(file, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != "DataConfiguration" {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, f := range st.Fields.List {
			if typ := exprString(f.Type); strings.HasSuffix(typ, ".Auth") && strings.HasPrefix(typ, "*") {
				auths = append(auths, strings.TrimSuffix(strings.TrimPrefix(typ, "*"), ".Auth"))
			}
		}

		return false
	})

	return auths
}

// countCalls counts the calls to the dotted name under a node.
func countCalls(n ast.Node, callee string) int {
	count := 0
	ast.Inspect(n, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok && exprString(c.Fun) == callee {
			count++
		}

		return true
	})

	return count
}

// countLiterals counts the composite literals of the named type under a node.
func countLiterals(n ast.Node, typeName string) int {
	count := 0
	ast.Inspect(n, func(n ast.Node) bool {
		if cl, ok := n.(*ast.CompositeLit); ok && exprString(cl.Type) == typeName {
			count++
		}

		return true
	})

	return count
}

// exprString renders an identifier, a selector chain or a pointer to one as the source
// writes it; any other expression renders empty.
func exprString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		if base := exprString(e.X); base != "" {
			return base + "." + e.Sel.Name
		}
	case *ast.StarExpr:
		if inner := exprString(e.X); inner != "" {
			return "*" + inner
		}
	}

	return ""
}
