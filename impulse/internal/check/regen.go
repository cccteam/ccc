package check

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
)

// regen re-runs code generation and fails when any generated file changes. The zz_gen
// files on disk are the golden output of the generators, so the comparison is content
// against content — it holds whether the tree is committed, dirty, or not yet tracked.
type regen struct{}

func (regen) Name() string { return "regen" }

func (regen) Describe() string {
	return "go generate ./... reproduces the generated files on disk (needs the Spanner emulator)"
}

func (c regen) Run(ctx context.Context, env *Env) Result {
	if env.SkipGenerate {
		return skip(c.Name(), "--skip-generate")
	}

	before, err := hashGenerated(env.App.Root)
	if err != nil {
		return fail(c.Name(), errors.Wrap(err, "hashGenerated()").Error())
	}

	env.notef("running go generate ./... (needs the Spanner emulator)")
	out, err := env.Exec.Run(ctx, env.App.Root, nil, "go", "generate", "./...")
	if err != nil {
		return fail(c.Name(), "go generate ./... failed", outputLines(out, 40)...)
	}

	after, err := hashGenerated(env.App.Root)
	if err != nil {
		return fail(c.Name(), errors.Wrap(err, "hashGenerated()").Error())
	}

	drift := generatedDrift(before, after)
	if len(drift) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d generated file(s) changed when regenerated; commit the regenerated files if the change is intentional", len(drift)), drift...)
	}

	return pass(c.Name(), "regeneration reproduces the generated files on disk")
}

// skippedDirs are trees that never hold generated sources and are expensive to walk.
var skippedDirs = map[string]bool{".git": true, "node_modules": true, ".angular": true, "dist": true, "vendor": true}

// hashGenerated maps every zz_gen file under dir, by slash-separated path relative to
// dir, to its content hash. The walk stays inside dir (os.Root), so a symlink cannot
// lead it out of the application.
func hashGenerated(dir string) (map[string][sha256.Size]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, errors.Wrap(err, "os.OpenRoot()")
	}
	defer root.Close()

	fsys := root.FS()
	sums := make(map[string][sha256.Size]byte)
	err = fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.Wrapf(err, "walking %q", path)
		}
		if d.IsDir() {
			if path != "." && skippedDirs[d.Name()] {
				return fs.SkipDir
			}

			return nil
		}
		if !strings.Contains(d.Name(), "zz_gen") {
			return nil
		}
		raw, err := fs.ReadFile(fsys, path)
		if err != nil {
			return errors.Wrapf(err, "fs.ReadFile(%q)", path)
		}
		sums[path] = sha256.Sum256(raw)

		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "fs.WalkDir()")
	}

	return sums, nil
}

// generatedDrift lists the generated files whose content differs between the two
// snapshots: changed, written, or removed by the regeneration. Sorted by path.
func generatedDrift(before, after map[string][sha256.Size]byte) []string {
	var drift []string
	for path, sum := range after {
		if prev, ok := before[path]; !ok {
			drift = append(drift, path+" (written)")
		} else if prev != sum {
			drift = append(drift, path+" (changed)")
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			drift = append(drift, path+" (removed)")
		}
	}
	slices.Sort(drift)

	return drift
}
