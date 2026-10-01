// Package migration renumbers a branch's own schema migrations. The pipeline's guard and
// bedrock check refuse a migrations directory that is not one sequence the migrate
// command applies in order: six-digit indexes, one up file each, contiguous, and in a
// pull-request build read together with the default branch's, so an index the default
// branch took since the branch was cut is refused. A branch opens two holes in that
// sequence, an index taken twice and a gap, and this closes both: the files the branch
// added move, up and down together, to follow the default branch's highest index with
// no gap, keeping their order. A committed schema migration is never touched. The seed
// directory's files are editable, so a committed seed file may be removed: the committed
// seed files after it move down to close the gap, and the branch's own follow them.
package migration

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
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
	// Editable names the directories of Dirs whose committed files may move: the seed
	// directory, whose files are editable, where the files after a removed one move
	// down to close its gap. A committed file elsewhere is never touched.
	Editable []string
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
// branch's, and in an editable directory the files after a removed one down. A directory
// that does not exist has nothing to renumber.
func Renumber(ctx context.Context, opts Options) (*Result, error) {
	ref, commit, err := defaultRef(ctx, opts.Root, opts.Branch)
	if err != nil {
		return nil, err
	}
	base, err := mergeBase(ctx, opts.Root, ref)
	if err != nil {
		return nil, err
	}
	n := &renumbering{root: opts.Root, ref: ref, base: base, branch: opts.Branch, result: &Result{Ref: ref, Commit: commit}}
	for _, dir := range opts.Dirs {
		if err := n.dir(ctx, dir, slices.Contains(opts.Editable, dir)); err != nil {
			return nil, err
		}
	}

	return n.result, nil
}

// renumbering is one run: the default branch's ref, the commit the branch was cut from it
// at, and the result the directories add to.
type renumbering struct {
	root, ref, base, branch string
	result                  *Result
}

// dir renumbers one directory: the files the working tree holds that the default branch
// does not are the branch's own; they follow the highest index the default branch holds
// there, or, when it holds none, their own lowest. An editable directory is packed
// instead (seedRenames).
func (n *renumbering) dir(ctx context.Context, dir string, editable bool) error {
	abs := filepath.Join(n.root, filepath.FromSlash(dir))
	entries, err := os.ReadDir(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return errors.Wrapf(err, "os.ReadDir(): %s", dir)
	}
	committed, err := treeFiles(ctx, n.root, n.ref, dir)
	if err != nil {
		return err
	}
	own := readStems(entries, func(name string) bool {
		return !committed[name]
	})
	for key, s := range own {
		if s.up {
			continue
		}
		why := "a down file with no up file"
		if committed[key+".up.sql"] {
			why = "a down file added for a committed migration"
		}
		n.result.Notes = append(n.result.Notes, fmt.Sprintf("%s: %s, left alone", path.Join(dir, key+".down.sql"), why))
	}
	sort.Strings(n.result.Notes)
	var pending []Rename
	if editable {
		pending, err = n.seedRenames(ctx, dir, entries, committed)
		if err != nil {
			return err
		}
	} else if stems := ordered(withUp(own)); len(stems) > 0 {
		first := stems[0].idx
		if highest, found := highestIndex(committed); found {
			first = highest + 1
		}
		pending = packed(dir, stems, first)
	}
	if len(pending) == 0 {
		return nil
	}
	tracked, err := trackedFiles(ctx, n.root, dir)
	if err != nil {
		return err
	}

	return n.result.apply(ctx, n.root, abs, tracked, pending)
}

// seedRenames is an editable directory's renames: every file the tree holds, in its order,
// packed from the lowest index with no gap, around the indexes the default branch took
// since the branch was cut (its files there that the tree does not hold), so a removed
// file leaves no gap and the branch's own files follow. A file the branch was cut with
// moves down only: when the default branch's additions would push one up, the directory
// is left alone with a note, as it is noted when they leave a gap only a merge closes.
func (n *renumbering) seedRenames(ctx context.Context, dir string, entries []os.DirEntry, committed map[string]bool) ([]Rename, error) {
	atBase, err := treeFiles(ctx, n.root, n.base, dir)
	if err != nil {
		return nil, err
	}
	local := map[string]bool{}
	for _, e := range entries {
		local[e.Name()] = true
	}
	taken := map[int]string{}
	for name := range committed {
		m := NameRE.FindStringSubmatch(name)
		if m == nil || local[name] || atBase[name] || m[3] != upKind {
			continue
		}
		idx, _ := strconv.Atoi(m[1])
		taken[idx] = m[1] + "_" + m[2]
	}
	stems := ordered(withUp(readStems(entries, func(string) bool {
		return true
	})))
	if len(stems) == 0 {
		return nil, nil
	}
	var pending []Rename
	next, pushed := stems[0].idx, ""
	for _, s := range stems {
		for taken[next] != "" {
			pushed = taken[next]
			next++
		}
		if next > s.idx && atBase[s.key()+".up.sql"] {
			n.result.Notes = append(n.result.Notes, fmt.Sprintf("%s: %s added %s since the branch was cut, and %s, which the branch was cut with, would move up past it; a committed seed file only moves down. Merge %s into the branch, then run the renumber again", dir, n.branch, pushed, s.key(), n.branch))

			return nil, nil
		}
		if next != s.idx {
			pending = append(pending, Rename{Dir: dir, From: s.key(), To: fmt.Sprintf("%06d_%s", next, s.name), Files: s.files()})
		}
		next++
	}
	above := make([]int, 0, len(taken))
	for idx := range taken {
		if idx >= next {
			above = append(above, idx)
		}
	}
	sort.Ints(above)
	for i, idx := range above {
		if idx != next+i {
			n.result.Notes = append(n.result.Notes, fmt.Sprintf("%s: a gap stays below %s, which %s added since the branch was cut; merge %s into the branch, then run the renumber again", dir, taken[idx], n.branch, n.branch))

			break
		}
	}

	return pending, nil
}

// withUp is the stems that have an up file: the ones that move.
func withUp(stems map[string]*stem) map[string]*stem {
	kept := map[string]*stem{}
	for key, s := range stems {
		if s.up {
			kept[key] = s
		}
	}

	return kept
}

// readStems reads the migration files among the entries that keep accepts, by stem.
func readStems(entries []os.DirEntry, keep func(name string) bool) map[string]*stem {
	stems := map[string]*stem{}
	for _, e := range entries {
		if e.IsDir() || !keep(e.Name()) {
			continue
		}
		m := NameRE.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		key := m[1] + "_" + m[2]
		s, ok := stems[key]
		if !ok {
			idx, _ := strconv.Atoi(m[1])
			s = &stem{idx: idx, name: m[2]}
			stems[key] = s
		}
		if m[3] == upKind {
			s.up = true
		} else {
			s.down = true
		}
	}

	return stems
}

// ordered sorts the stems by index, then name.
func ordered(stems map[string]*stem) []*stem {
	list := make([]*stem, 0, len(stems))
	for _, s := range stems {
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].idx != list[j].idx {
			return list[i].idx < list[j].idx
		}

		return list[i].name < list[j].name
	})

	return list
}

// packed is the renames that put the stems at start, start+1, ... in their order.
func packed(dir string, stems []*stem, start int) []Rename {
	var pending []Rename
	for i, s := range stems {
		if want := start + i; s.idx != want {
			pending = append(pending, Rename{Dir: dir, From: s.key(), To: fmt.Sprintf("%06d_%s", want, s.name), Files: s.files()})
		}
	}

	return pending
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
