// Package check compares a committed application stack with what the code declares: it
// renders the stack afresh and reports every owned file whose committed content differs,
// which is the drift between the code and the infrastructure.
package check

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/render"
)

// Finding is one file that is not as the render says it should be.
type Finding struct {
	// Path is the file's path relative to the stack directory, or to the application
	// root when Root is set.
	Path string
	// Root is true for a file at the application root (the pipeline files).
	Root bool
	// Missing reports a file the render produces that the directory lacks.
	Missing bool
	// Line is the first differing line (1-based), 0 when Missing.
	Line int
	// Want and Got are that line as rendered and as committed.
	Want string
	Got  string
}

// Authoritative is an authoritative IAM resource (a *_iam_binding or *_iam_policy)
// declared in one of the stack's files. The stack refuses them: such a resource
// replaces every member of its role or policy on each apply, so a pull-request stack
// applying one would remove the environment's members, and two pull requests each
// other's. A *_iam_member adds one member and removes only that one.
type Authoritative struct {
	// Path is the file's path relative to the stack directory.
	Path string
	// Line is the resource block's line (1-based).
	Line int
	// Address is the resource's type.name.
	Address string
}

// Report is the outcome of one check.
type Report struct {
	// Dir is the stack directory checked, and AppDir the application root its pipeline
	// files were checked at.
	Dir    string
	AppDir string
	// Checked counts the owned files compared.
	Checked int
	// Findings are the owned files that differ or are missing, in path order.
	Findings []Finding
	// Unseeded lists the seeded files the directory lacks: not drift, since the tool
	// writes them once and a person keeps them, but worth a line.
	Unseeded []string
	// Authoritative lists the authoritative IAM resources declared anywhere in the
	// stack, owned files and a person's alike, in path then line order.
	Authoritative []Authoritative
	// Migrations are the problems with the schema migrations directory: a file that
	// is not a migration, an index with two up files, a gap in the sequence.
	Migrations []MigrationFinding
}

// Clean reports no drift, no refused resource and a sound migration sequence.
func (r *Report) Clean() bool {
	return len(r.Findings) == 0 && len(r.Authoritative) == 0 && len(r.Migrations) == 0
}

// Run renders the model and compares the owned files with the directory's, and the
// owned files at the application root with appDir's. It also reads the schema migrations
// directory and the seed directory beside it for a sequence the migrate command could not
// apply in order.
func Run(m *derive.Model, dir, appDir string) (*Report, error) {
	files, err := render.Render(m)
	if err != nil {
		return nil, err
	}
	r := &Report{Dir: dir, AppDir: appDir}
	for _, f := range files {
		in := dir
		if f.Root {
			in = appDir
		}
		committed, err := os.ReadFile(filepath.Join(in, filepath.FromSlash(f.Path)))
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
			r.Findings = append(r.Findings, Finding{Path: f.Path, Root: f.Root, Missing: true})

			continue
		}
		if line, want, got, same := firstDifference(f.Content, committed); !same {
			r.Findings = append(r.Findings, Finding{Path: f.Path, Root: f.Root, Line: line, Want: want, Got: got})
		}
	}
	authoritative, err := scanAuthoritative(dir)
	if err != nil {
		return nil, err
	}
	r.Authoritative = authoritative
	for _, d := range migrationDirs(m) {
		migrations, err := scanMigrations(filepath.Join(appDir, filepath.FromSlash(d)), d)
		if err != nil {
			return nil, err
		}
		r.Migrations = append(r.Migrations, migrations...)
	}

	return r, nil
}

// migrationDirs is the schema migrations directory and the seed directory beside it
// (schema/devseed, the data migrations the migrate command applies with -seed), both
// root-relative; none when the application has no schema.
func migrationDirs(m *derive.Model) []string {
	if m.Schema.MigrationsDir == "" {
		return nil
	}

	return []string{m.Schema.MigrationsDir, path.Join(path.Dir(m.Schema.MigrationsDir), derive.SeedDir)}
}

// authoritativeResource matches the opening line of an authoritative IAM resource
// block: resource "<type>_iam_binding" "<name>" or resource "<type>_iam_policy" "<name>".
var authoritativeResource = regexp.MustCompile(`^\s*resource\s+"([A-Za-z0-9_]+_iam_(?:binding|policy))"\s+"([^"]+)"`)

// scanAuthoritative finds the authoritative IAM resources in every .tf file of the
// directory, a person's files included.
func scanAuthoritative(dir string) ([]Authoritative, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.tf"))
	if err != nil {
		return nil, errors.Wrap(err, "filepath.Glob()")
	}
	var found []Authoritative
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, errors.Wrapf(err, "os.ReadFile(): %s", path)
		}
		for i, line := range strings.Split(string(data), "\n") {
			m := authoritativeResource.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			found = append(found, Authoritative{Path: filepath.Base(path), Line: i + 1, Address: m[1] + "." + m[2]})
		}
	}

	return found, nil
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
	if len(r.Findings) == 0 {
		fmt.Fprintf(w, "%s (and the pipeline at %s): %d owned file(s) match the code\n", r.Dir, r.AppDir, r.Checked)
	} else {
		fmt.Fprintf(w, "%s (and the pipeline at %s): %d of %d owned file(s) differ from the code\n", r.Dir, r.AppDir, len(r.Findings), r.Checked)
	}
	for _, f := range r.Findings {
		where := ""
		if f.Root {
			where = " (at the application root)"
		}
		if f.Missing {
			fmt.Fprintf(w, "  missing  %s%s\n", f.Path, where)

			continue
		}
		fmt.Fprintf(w, "  differs  %s:%d%s\n", f.Path, f.Line, where)
		fmt.Fprintf(w, "           code:      %s\n", f.Want)
		fmt.Fprintf(w, "           committed: %s\n", f.Got)
	}
	for _, path := range r.Unseeded {
		fmt.Fprintf(w, "  unseeded %s (bedrock render creates it once)\n", path)
	}
	for _, a := range r.Authoritative {
		fmt.Fprintf(w, "  refused  %s:%d %s: an authoritative IAM resource replaces every member on each apply; declare a *_iam_member per member instead\n", a.Path, a.Line, a.Address)
	}
	for _, mf := range r.Migrations {
		fmt.Fprintf(w, "  refused  %s: %s\n", mf.Path, mf.Problem)
	}
}
