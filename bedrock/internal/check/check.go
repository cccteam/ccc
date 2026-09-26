// Package check compares a committed application stack with what the code declares: it
// renders the stack afresh and reports every owned file whose committed content differs,
// which is the drift between the code and the infrastructure.
package check

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/render"
)

// Finding is one file that is not as the render says it should be.
type Finding struct {
	// Path is the file's path relative to the stack directory.
	Path string
	// Missing reports a file the render produces that the directory lacks.
	Missing bool
	// Line is the first differing line (1-based), 0 when Missing.
	Line int
	// Want and Got are that line as rendered and as committed.
	Want string
	Got  string
}

// Report is the outcome of one check.
type Report struct {
	// Dir is the directory checked.
	Dir string
	// Checked counts the owned files compared.
	Checked int
	// Findings are the owned files that differ or are missing, in path order.
	Findings []Finding
	// Unseeded lists the seeded files the directory lacks: not drift, since the tool
	// writes them once and a person keeps them, but worth a line.
	Unseeded []string
}

// Clean reports no drift.
func (r *Report) Clean() bool {
	return len(r.Findings) == 0
}

// Run renders the model and compares the owned files with the directory's.
func Run(m *derive.Model, dir string) (*Report, error) {
	files, err := render.Render(m)
	if err != nil {
		return nil, err
	}
	r := &Report{Dir: dir}
	for _, f := range files {
		committed, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, errors.Wrapf(err, "os.ReadFile(): %s", f.Path)
		}
		if f.Tier == render.Seeded {
			if err != nil {
				r.Unseeded = append(r.Unseeded, f.Path)
			}

			continue
		}
		r.Checked++
		if err != nil {
			r.Findings = append(r.Findings, Finding{Path: f.Path, Missing: true})

			continue
		}
		if line, want, got, same := firstDifference(f.Content, committed); !same {
			r.Findings = append(r.Findings, Finding{Path: f.Path, Line: line, Want: want, Got: got})
		}
	}

	return r, nil
}

// firstDifference finds the first line where the two texts part, or reports them the
// same.
func firstDifference(want, got []byte) (line int, wantLine, gotLine string, same bool) {
	if bytes.Equal(want, got) {
		return 0, "", "", true
	}
	wantLines := strings.Split(string(want), "\n")
	gotLines := strings.Split(string(got), "\n")
	for i := range max(len(wantLines), len(gotLines)) {
		var w, g string
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g || i >= len(wantLines) || i >= len(gotLines) {
			return i + 1, w, g, false
		}
	}

	return len(wantLines), "", "", false
}

// Write prints the report the way a person reads it: one line per file, the first
// differing line under each.
func (r *Report) Write(w io.Writer) {
	if r.Clean() {
		fmt.Fprintf(w, "%s: %d owned file(s) match the code\n", r.Dir, r.Checked)
	} else {
		fmt.Fprintf(w, "%s: %d of %d owned file(s) differ from the code\n", r.Dir, len(r.Findings), r.Checked)
	}
	for _, f := range r.Findings {
		if f.Missing {
			fmt.Fprintf(w, "  missing  %s\n", f.Path)

			continue
		}
		fmt.Fprintf(w, "  differs  %s:%d\n", f.Path, f.Line)
		fmt.Fprintf(w, "           code:      %s\n", f.Want)
		fmt.Fprintf(w, "           committed: %s\n", f.Got)
	}
	for _, path := range r.Unseeded {
		fmt.Fprintf(w, "  unseeded %s (bedrock render creates it once)\n", path)
	}
}
