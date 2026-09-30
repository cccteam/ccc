// hooks.go reads the application's hooks: the scripts it commits under
// infrastructure/hooks and the hooks program at cmd/deployment/hooks.

package derive

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/hook"
	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/deployhook"
)

const (
	// hooksDir is the hooks program's main package.
	hooksDir = "cmd/deployment/hooks"
	// deployhookPath is the contract package the program implements its hooks through.
	deployhookPath = "github.com/cccteam/ccc/impulse/deployhook"
	// hooksType is the contract's literal the stages are read from.
	hooksType = "Hooks"
)

// HookProgram is the application's hooks program: its main package, and the stages its
// deployhook.Hooks literal implements, in the pipeline's order.
type HookProgram struct {
	Dir    string
	Stages []hook.Stage
}

// hookFields are the literal's fields by the stage each implements.
var hookFields = map[string]deployhook.Stage{
	"BeforeMigrate": deployhook.BeforeMigrate,
	"AfterMigrate":  deployhook.AfterMigrate,
	"BeforeTraffic": deployhook.BeforeTraffic,
	"AfterTraffic":  deployhook.AfterTraffic,
}

// hooks reads the hook scripts and the hooks program, and refuses a stage both implement:
// the pipeline runs one or the other.
func (m *Model) hooks(a *app.App) error {
	scripts, err := hook.Scripts(a.Root)
	if err != nil {
		return err
	}
	m.Hooks = scripts
	if !slices.Contains(a.MainPackages, hooksDir) {
		return nil
	}
	stages, err := programStages(a)
	if err != nil {
		return err
	}
	for _, s := range stages {
		if slices.Contains(scripts, s) {
			return errors.Newf("%s and %s both implement the %s hook: the pipeline runs one or the other, so keep one", s.Script(), hooksDir, s)
		}
	}
	m.HookProgram = &HookProgram{Dir: hooksDir, Stages: stages}

	return nil
}

// programStages reads the stages the hooks program implements from its one
// deployhook.Hooks literal: the keyed fields set to something other than nil.
func programStages(a *app.App) ([]hook.Stage, error) {
	dir := a.Abs(hooksDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, errors.Wrapf(err, "os.ReadDir(): %s", hooksDir)
	}
	var literals []*ast.CompositeLit
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		rel := path.Join(hooksDir, name)
		src, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			return nil, errors.Wrapf(err, "os.ReadFile(): %s", rel)
		}
		f, err := parser.ParseFile(token.NewFileSet(), rel, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, errors.Wrap(err, "parser.ParseFile()")
		}
		literals = append(literals, hooksLiterals(f)...)
	}
	if len(literals) != 1 {
		return nil, errors.Newf("%s holds %d %s.%s literals; bedrock reads the stages the program implements from its one literal, keyed by field (deployhook.Hooks{AfterMigrate: backfill})", hooksDir, len(literals), path.Base(deployhookPath), hooksType)
	}
	var stages []hook.Stage
	for _, elt := range literals[0].Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return nil, errors.Newf("%s's %s.%s literal must be keyed by field (BeforeTraffic: checkNextRevision)", hooksDir, path.Base(deployhookPath), hooksType)
		}
		key, _ := kv.Key.(*ast.Ident)
		if value, isIdent := kv.Value.(*ast.Ident); key == nil || (isIdent && value.Name == "nil") {
			continue
		}
		if stage, ok := hookFields[key.Name]; ok {
			stages = append(stages, hook.Stage(stage))
		}
	}
	order := map[hook.Stage]int{}
	for i, s := range hook.Stages {
		order[s] = i
	}
	sort.Slice(stages, func(i, j int) bool {
		return order[stages[i]] < order[stages[j]]
	})

	return stages, nil
}

// hooksLiterals are the file's composite literals of the contract's Hooks type, through
// whatever name the file imports the contract under.
func hooksLiterals(f *ast.File) []*ast.CompositeLit {
	local := importName(f, deployhookPath)
	if local == "" {
		return nil
	}
	var found []*ast.CompositeLit
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if sel, ok := lit.Type.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == local && sel.Sel.Name == hooksType {
				found = append(found, lit)
			}
		}

		return true
	})

	return found
}
