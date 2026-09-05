package check

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"
)

// rpcExecute verifies that every generated RPC handler calls the method's Execute. When
// the generator cannot type-check an RPC method it emits a decode-only handler that
// answers requests without running the method: a fail-open bug that compiles cleanly.
type rpcExecute struct{}

func (rpcExecute) Name() string { return "rpc-execute" }

func (rpcExecute) Describe() string {
	return "every generated RPC handler calls the method's Execute"
}

func (c rpcExecute) Run(_ context.Context, env *Env) Result {
	a := env.App
	var details []string
	checked, headerless := 0, 0

	for _, g := range a.Generators {
		handlers, rpcDir := g.HandlersDir(), g.RPCDir()
		if handlers == "" || rpcDir == "" {
			continue
		}
		files, err := filepath.Glob(filepath.Join(a.Abs(handlers), "zz_gen_*.go"))
		if err != nil {
			return fail(c.Name(), errors.Wrap(err, "filepath.Glob()").Error())
		}
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				return fail(c.Name(), errors.Wrap(err, "os.ReadFile()").Error())
			}
			source := sourceOf(data)
			if source == "" {
				headerless++

				continue
			}
			if source != rpcDir {
				continue
			}
			checked++
			if !strings.Contains(string(data), ".Execute(") {
				details = append(details, a.Rel(f)+": no Execute call; the handler decodes and returns without running the method")
			}
		}
	}

	if checked == 0 && headerless > 0 {
		return warn(c.Name(), fmt.Sprintf("cannot identify RPC handlers: %d zz_gen file(s) in the handlers directory carry no Source header, so the resource generator did not write them", headerless))
	}
	if checked == 0 {
		return skip(c.Name(), "no generated RPC handlers")
	}
	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d of %d RPC handler(s) never call Execute (regenerate and read the generator output)", len(details), checked), details...)
	}

	return pass(c.Name(), fmt.Sprintf("%d RPC handler(s) call Execute", checked))
}

// sourceOf returns the directory named by the generated file's "// Source:" header line,
// cleaned, or empty.
func sourceOf(data []byte) string {
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if src, ok := strings.CutPrefix(line, "// Source:"); ok {
			return path.Clean(strings.TrimSpace(src))
		}
		if line != "" && !strings.HasPrefix(line, "//") {
			return ""
		}
	}

	return ""
}
