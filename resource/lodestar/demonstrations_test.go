package main

// demonstrations_test is the demonstration index (design plan §8, §9, ruled 2026-09-09):
// every capability the resource package declares in its registry must be proven here, in
// code and in a test, and say so. The test walks this tree for "Demonstrates: key, key."
// paragraphs, fails on a registry key with no declaration in a non-test file or in a test,
// on a declaration naming a key the registry does not know, and on a README example link
// pointing at a file that does not declare the key its row documents; run with
// LODESTAR_UPDATE_DEMONSTRATIONS=1 it rewrites DEMONSTRATIONS.md, and otherwise asserts the
// committed table is current.
//
// This file is its own declaration of the index it audits (the scanner skips the file, so
// the keys below are recorded here): demonstration-index, ci-stub.

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource"
)

// declarationFiles are the file kinds a Demonstrates paragraph may live in: Go, the
// browser sources, the README's persona rows, the walkthrough, and the role files' doc.
var declarationFiles = []string{".go", ".ts", ".html", ".md", ".sh", ".json"}

// skippedDirs are never walked.
var skippedDirs = map[string]bool{"node_modules": true, "dist": true, ".angular": true, ".ccc-cache": true, ".yalc": true, "uploads": true}

// A declaration is "Demonstrates:" followed by keys, not the word quoted in prose (a
// backtick before it), and a markdown table cell may close the line.
var demonstratesLine = regexp.MustCompile("(?:^|[^`])Demonstrates:\\s*([^\\n|]*?)\\.?\\s*\\|?\\s*$")

// declaration is one Demonstrates paragraph: where it was and what it names.
type declaration struct {
	file string
	keys []string
	test bool
}

// scanDeclarations walks the tree for Demonstrates paragraphs.
func scanDeclarations(t *testing.T, root string) []declaration {
	t.Helper()

	// Files are read through a root confined to the tree, never by the walked path.
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("os.OpenRoot(%s): %v", root, err)
	}
	defer rootFS.Close()

	var out []declaration
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skippedDirs[d.Name()] {
				return filepath.SkipDir
			}

			return nil
		}
		if !slices.Contains(declarationFiles, filepath.Ext(path)) || strings.Contains(d.Name(), "zz_gen") || d.Name() == "DEMONSTRATIONS.md" || d.Name() == "demonstrations_test.go" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("filepath.Rel(%s): %w", path, err)
		}
		f, err := rootFS.Open(rel)
		if err != nil {
			return fmt.Errorf("os.Root.Open(%s): %w", rel, err)
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			m := demonstratesLine.FindStringSubmatch(scanner.Text())
			if m == nil {
				continue
			}
			var keys []string
			for key := range strings.SplitSeq(strings.TrimSuffix(strings.TrimSpace(m[1]), "*/"), ",") {
				if key = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(key), ".")); key != "" {
					keys = append(keys, key)
				}
			}
			// The walkthrough is a proof run by hand against a fresh stack: a test.
			test := strings.HasSuffix(rel, "_test.go") || strings.HasPrefix(rel, "test/") || filepath.Ext(rel) == ".sh"
			out = append(out, declaration{file: rel, keys: keys, test: test})
		}

		return scanner.Err()
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}

	// The CI stub lives one directory up, in the resource module; it is part of the proof.
	stub := filepath.Join(root, "..", "lodestar_test.go")
	if raw, err := os.ReadFile(stub); err == nil {
		for line := range strings.SplitSeq(string(raw), "\n") {
			if m := demonstratesLine.FindStringSubmatch(line); m != nil {
				var keys []string
				for key := range strings.SplitSeq(m[1], ",") {
					if key = strings.TrimSpace(key); key != "" {
						keys = append(keys, key)
					}
				}
				out = append(out, declaration{file: "../lodestar_test.go", keys: keys, test: true})
			}
		}
	}

	return out
}

// TestDemonstrations is the audit: every registry key declared in code and in a test,
// every declared key known, every README example link on a declaring file, and the
// generated table current.
func TestDemonstrations(t *testing.T) {
	t.Parallel()

	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	registry := resource.DemonstrationDescriptions()
	decls := scanDeclarations(t, root)

	inCode := map[string][]string{}
	inTests := map[string][]string{}
	for _, d := range decls {
		for _, key := range d.keys {
			if _, known := registry[key]; !known {
				t.Errorf("%s declares %q, which the registry does not know", d.file, key)

				continue
			}
			if d.test {
				inTests[key] = append(inTests[key], d.file)
			} else {
				inCode[key] = append(inCode[key], d.file)
			}
		}
	}
	for _, key := range resource.DemonstrationKeys() {
		if len(inCode[key]) == 0 {
			t.Errorf("registry key %q is declared by no non-test file", key)
		}
		if len(inTests[key]) == 0 {
			t.Errorf("registry key %q is declared by no test", key)
		}
	}

	// README example links: [text](path) rows whose text is a registry key must point
	// at a file declaring that key.
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	link := regexp.MustCompile(`\[` + "`" + `([^` + "`" + `\]]+)` + "`" + `\]\(([^)]+)\)`)
	for _, m := range link.FindAllStringSubmatch(string(readme), -1) {
		key, target := m[1], m[2]
		if _, known := registry[key]; !known {
			continue
		}
		target = strings.SplitN(target, "#", 2)[0]
		files := append(slices.Clone(inCode[key]), inTests[key]...)
		if !slices.Contains(files, target) {
			t.Errorf("README links %q to %s, which does not declare it (declared in %v)", key, target, files)
		}
	}

	table := renderTable(registry, inCode, inTests)
	path := filepath.Join(root, "DEMONSTRATIONS.md")
	if os.Getenv("LODESTAR_UPDATE_DEMONSTRATIONS") != "" {
		if err := os.WriteFile(path, []byte(table), 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}
	committed, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v (run with LODESTAR_UPDATE_DEMONSTRATIONS=1 to write it)", path, err)
	}
	if string(committed) != table {
		t.Error("DEMONSTRATIONS.md is out of date; run with LODESTAR_UPDATE_DEMONSTRATIONS=1 to regenerate it")
	}
}

// renderTable writes the key-to-files table.
func renderTable(registry map[string]string, inCode, inTests map[string][]string) string {
	var b strings.Builder
	b.WriteString("# What Lodestar demonstrates\n\n")
	b.WriteString("Generated by `demonstrations_test.go` from the resource package's demonstration registry and the\n")
	b.WriteString("`Demonstrates:` paragraphs in this tree. Do not edit; run\n")
	b.WriteString("`LODESTAR_UPDATE_DEMONSTRATIONS=1 go test -run TestDemonstrations .` to regenerate.\n\n")
	b.WriteString("| Key | What it is | Proven by | Tested by |\n| --- | --- | --- | --- |\n")
	keys := make([]string, 0, len(registry))
	for key := range registry {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", key, registry[key], files(inCode[key]), files(inTests[key]))
	}

	return b.String()
}

func files(paths []string) string {
	paths = slices.Clone(paths)
	sort.Strings(paths)
	paths = slices.Compact(paths)
	if len(paths) == 0 {
		return "—"
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, fmt.Sprintf("[%s](%s)", filepath.Base(p), p))
	}

	return strings.Join(out, ", ")
}
