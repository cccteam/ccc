// Package migration renumbers a branch's own schema migrations. The pipeline's guard and
// bedrock check refuse a migrations directory that is not one sequence the migrate
// command applies in order: six-digit indexes, one up file each, contiguous, and in a
// pull-request build read together with the default branch's, so an index the default
// branch took since the branch was cut is refused. A branch opens two holes in that
// sequence, an index taken twice and a gap, and this closes both: the files the branch
// added move, up and down together, to follow the default branch's highest index with
// no gap, keeping their order. A committed migration is never touched.
package migration

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"github.com/go-playground/errors/v5"
)

// Options names the repository, the directories and the branch.
type Options struct {
	// Root is the repository root; Dirs are the migration directories, root-relative:
	// the schema migrations and the seed directory beside them.
	Root string
	Dirs []string
	// Branch is the default branch, whose committed sequence the branch's own files
	// follow. Its tree is read from origin's copy of the branch when the repository
	// has one, else from the local branch.
	Branch string
}

// Rename is one migration moved: its stem (NNNNNN_name) before and after, in Dir, and
// which of its files (up, down) moved with it.
type Rename struct {
	Dir   string
	From  string
	To    string
	Files []string
}

// Result is what the renumber read and did.
type Result struct {
	// Ref is the ref the committed sequence was read from, at Commit (short).
	Ref     string
	Commit  string
	Renames []Rename
	// Notes name the files left alone, and why.
	Notes []string
}

// NameRE is a migration file name: a six-digit index, an underscore, a name, and .up.sql
// or .down.sql. The index is the first group, the name the second, the kind the third.
var NameRE = regexp.MustCompile(`^(\d{6})_([A-Za-z0-9_-]+)\.(up|down)\.sql$`)

// The two kinds of migration file.
const (
	upKind   = "up"
	downKind = "down"
)

// stem is one migration of the branch's own: its index and name, and which files it has.
type stem struct {
	idx      int
	name     string
	up, down bool
}

func (s *stem) key() string {
	return fmt.Sprintf("%06d_%s", s.idx, s.name)
}

func (s *stem) files() []string {
	var files []string
	if s.up {
		files = append(files, upKind)
	}
	if s.down {
		files = append(files, downKind)
	}

	return files
}

// Renumber moves the branch's own migrations in every directory to follow the default
// branch's. A directory that does not exist has nothing to renumber.
func Renumber(ctx context.Context, opts Options) (*Result, error) {
	ref, commit, err := defaultRef(ctx, opts.Root, opts.Branch)
	if err != nil {
		return nil, err
	}
	r := &Result{Ref: ref, Commit: commit}
	for _, dir := range opts.Dirs {
		if err := r.renumberDir(ctx, opts.Root, ref, dir); err != nil {
			return nil, err
		}
	}

	return r, nil
}

// renumberDir renumbers one directory: the files the working tree holds that the
// default branch does not are the branch's own; they follow the highest index the
// default branch holds there, or, when it holds none, their own lowest.
func (r *Result) renumberDir(ctx context.Context, root, ref, dir string) error {
	abs := filepath.Join(root, filepath.FromSlash(dir))
	entries, err := os.ReadDir(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return errors.Wrapf(err, "os.ReadDir(): %s", dir)
	}
	committed, err := treeFiles(ctx, root, ref, dir)
	if err != nil {
		return err
	}
	own := map[string]*stem{}
	for _, e := range entries {
		if e.IsDir() || committed[e.Name()] {
			continue
		}
		m := NameRE.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		key := m[1] + "_" + m[2]
		s, ok := own[key]
		if !ok {
			idx, _ := strconv.Atoi(m[1])
			s = &stem{idx: idx, name: m[2]}
			own[key] = s
		}
		if m[3] == upKind {
			s.up = true
		} else {
			s.down = true
		}
	}
	stems := make([]*stem, 0, len(own))
	for key, s := range own {
		if !s.up {
			why := "a down file with no up file"
			if committed[key+".up.sql"] {
				why = "a down file added for a committed migration"
			}
			r.Notes = append(r.Notes, fmt.Sprintf("%s: %s, left alone", path.Join(dir, key+".down.sql"), why))

			continue
		}
		stems = append(stems, s)
	}
	sort.Slice(stems, func(i, j int) bool {
		if stems[i].idx != stems[j].idx {
			return stems[i].idx < stems[j].idx
		}

		return stems[i].name < stems[j].name
	})
	sort.Strings(r.Notes)
	if len(stems) == 0 {
		return nil
	}
	start := stems[0].idx
	if base, ok := highestIndex(committed); ok {
		start = base + 1
	}
	var pending []Rename
	for i, s := range stems {
		if want := start + i; s.idx != want {
			pending = append(pending, Rename{Dir: dir, From: s.key(), To: fmt.Sprintf("%06d_%s", want, s.name), Files: s.files()})
		}
	}
	if len(pending) == 0 {
		return nil
	}
	tracked, err := trackedFiles(ctx, root, dir)
	if err != nil {
		return err
	}

	return r.apply(ctx, root, abs, tracked, pending)
}

// apply performs the renames in an order that never moves a file onto one still to
// move: a target that is another rename's source waits for it. The sources are distinct
// and the targets are distinct and increasing with them, so the waits form chains, not
// cycles, and every pass moves at least one.
func (r *Result) apply(ctx context.Context, root, abs string, tracked map[string]bool, pending []Rename) error {
	for len(pending) > 0 {
		moved := false
		for i, p := range pending {
			if blocked(p, pending) {
				continue
			}
			for _, kind := range p.Files {
				from, to := p.From+"."+kind+".sql", p.To+"."+kind+".sql"
				if _, err := os.Stat(filepath.Join(abs, to)); err == nil {
					return errors.Newf("%s: cannot rename %s to %s: a file of that name exists", p.Dir, from, to)
				}
				if tracked[from] {
					if err := gitMove(ctx, root, path.Join(p.Dir, from), path.Join(p.Dir, to)); err != nil {
						return err
					}
				} else if err := os.Rename(filepath.Join(abs, from), filepath.Join(abs, to)); err != nil {
					return errors.Wrapf(err, "os.Rename(): %s", path.Join(p.Dir, from))
				}
			}
			r.Renames = append(r.Renames, p)
			pending = append(pending[:i], pending[i+1:]...)
			moved = true

			break
		}
		if !moved {
			return errors.Newf("%s: the renames wait on each other", pending[0].Dir)
		}
	}

	return nil
}

// blocked reports whether the rename's target is another pending rename's source.
func blocked(p Rename, pending []Rename) bool {
	for _, q := range pending {
		if q.From == p.To {
			return true
		}
	}

	return false
}

// highestIndex is the highest migration index among the names, when any is one.
func highestIndex(names map[string]bool) (int, bool) {
	highest, found := 0, false
	for name := range names {
		m := NameRE.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		idx, _ := strconv.Atoi(m[1])
		if !found || idx > highest {
			highest, found = idx, true
		}
	}

	return highest, found
}
