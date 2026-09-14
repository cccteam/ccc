package skeleton

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/go-playground/errors/v5"
)

// retiredWords are the names the vocabulary freeze retired before impulse's first release.
// They must not return through a copied paragraph, so the test scans everything an adopter
// reads: the embedded candidates, the tool's README, and the module's non-test Go source,
// fixtures included, with no exception list.
var retiredWords = []string{
	"single-site", "multi-site", "ServerConfiguration", "serverConfig", "server level",
	"user pool", "--sessions", "--first", "--table", "auth-wired",
	"Administrator role at each scope",
}

// Lockfiles and binary assets are not prose. The candidates on disk are the embedded ones,
// scanned through the embed FS, so the walk over the module skips their directory.
var (
	lockfiles   = map[string]bool{"bun.lock": true, "package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true}
	skippedDirs = map[string]bool{"node_modules": true, ".git": true, dir: true}
)

func TestRetiredWords(t *testing.T) {
	t.Parallel()

	var findings []string
	report := func(file, text string) {
		for i, line := range strings.Split(text, "\n") {
			for _, w := range retiredWords {
				if strings.Contains(line, w) {
					findings = append(findings, fmt.Sprintf("%s:%d: %q", file, i+1, w))
				}
			}
		}
	}

	err := fs.WalkDir(root, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.Wrap(err, "fs.WalkDir()")
		}
		if d.IsDir() || lockfiles[d.Name()] {
			return nil
		}
		data, err := fs.ReadFile(root, p)
		if err != nil {
			return errors.Wrap(err, "fs.ReadFile()")
		}
		if utf8.Valid(data) {
			report(p, string(data))
		}

		return nil
	})
	if err != nil {
		t.Fatalf("the embedded candidates: %v", err)
	}

	module, err := os.OpenRoot(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	readme, err := module.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	report("README.md", string(readme))

	err = fs.WalkDir(module.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.Wrap(err, "fs.WalkDir()")
		}
		if d.IsDir() {
			if p != "." && skippedDirs[d.Name()] {
				return fs.SkipDir
			}

			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		data, err := module.ReadFile(filepath.FromSlash(p))
		if err != nil {
			return errors.Wrap(err, "os.Root.ReadFile()")
		}
		report(p, string(data))

		return nil
	})
	if err != nil {
		t.Fatalf("the module's Go source: %v", err)
	}

	if len(findings) > 0 {
		t.Errorf("%d retired word(s); the vocabulary is frozen (README, Vocabulary):\n%s", len(findings), strings.Join(findings, "\n"))
	}
}
