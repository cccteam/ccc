// check.go compares a committed organization repository with what the placement
// renders: the drift between the placement and the layers.

package org

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/render"
)

// Finding is one owned file that is not as the render says it should be.
type Finding struct {
	// Path is the file's path relative to the repository root.
	Path string
	// Missing reports a file the render produces that the repository lacks.
	Missing bool
	// Line is the first differing line (1-based), 0 when Missing.
	Line int
	// Want and Got are that line as rendered and as committed.
	Want string
	Got  string
}

// Report is the outcome of one check.
type Report struct {
	// Dir is the repository directory checked.
	Dir string
	// Checked counts the owned files compared.
	Checked int
	// Findings are the owned files that differ or are missing, in path order.
	Findings []Finding
	// Unseeded lists the seeded files the repository lacks: not drift, since the tool
	// writes them once and a person keeps them, but worth a line.
	Unseeded []string
}

// Clean reports no drift.
func (r *Report) Clean() bool {
	return len(r.Findings) == 0
}

// Check renders the placement and compares the owned files with the repository's.
func Check(p *Placement, dir string) (*Report, error) {
	files, err := Render(p)
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

// firstDifference finds the first line where the rendered and committed contents part.
func firstDifference(want, got []byte) (line int, wantLine, gotLine string, same bool) {
	if bytes.Equal(want, got) {
		return 0, "", "", true
	}
	wantLines := bytes.Split(want, []byte("\n"))
	gotLines := bytes.Split(got, []byte("\n"))
	for i := range max(len(wantLines), len(gotLines)) {
		var w, g []byte
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if !bytes.Equal(w, g) {
			return i + 1, string(w), string(g), false
		}
	}

	return 0, "", "", true
}
