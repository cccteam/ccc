// consolidate.go folds the whole schema sequence into one file carrying the latest index.
// A database holds one number, the highest migration applied, and a run applies every
// file with a higher number in order, comparing no content: a database at the latest
// index applies nothing after the fold, a fresh one runs the one file and gets the same
// schema, and the runner sends a file's statements as one batch per kind, so the fold is
// the speed. The file is the committed up files in order, each followed by a semicolon
// and a comment naming the file it came from, and the down file the down files in
// reverse order: identical by construction, data migrations included.

package migration

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/gitcmd"
)

// ConsolidatedName is the fixed name of the folded file, after its index: the migrator
// reads the index alone, the pipeline reads the directory's shape, and the name tells the
// reader of the listing what the file is.
const ConsolidatedName = "Consolidated"

// ConsolidateOptions names the repository, the schema migrations directory and the branch.
type ConsolidateOptions struct {
	// Root is the repository root; Dir the schema migrations directory, root-relative.
	Root string
	Dir  string
	// Branch is the default branch, whose committed sequence is folded; a branch cut from
	// a patch line follows the line instead (followedBranch).
	Branch string
}

// Consolidation is what the fold read and did.
type Consolidation struct {
	// Ref is the ref the committed sequence was read from, at Commit (short).
	Ref    string
	Commit string
	Dir    string
	// Index is the latest index, which the folded file carries; Up and Down name the
	// files written (Down is empty when no folded migration had a down file), and Folded
	// the stems folded into them, in order. Nothing was done when Folded is empty.
	Index  int
	Up     string
	Down   string
	Folded []string
	// WithoutDown names the folded stems that had no down file: the down file reverses
	// the others.
	WithoutDown []string
}

// Consolidate folds the directory's sequence, as the followed branch holds it, into one
// up file and one down file at the latest index, removes the folded files and stages the
// change. It refuses a directory that is not as the followed branch holds it (a change
// under it in the working tree, or a migration of the branch's own): a consolidation is a
// pull request of its own, cut from the branch it folds. A sequence of one file is left
// alone.
func Consolidate(ctx context.Context, opts ConsolidateOptions) (*Consolidation, error) {
	branch, err := followedBranch(ctx, opts.Root, opts.Branch)
	if err != nil {
		return nil, err
	}
	ref, commit, err := gitcmd.DefaultRef(ctx, opts.Root, branch)
	if err != nil {
		return nil, err
	}
	c := &Consolidation{Ref: ref, Commit: commit, Dir: opts.Dir}
	abs := filepath.Join(opts.Root, filepath.FromSlash(opts.Dir))
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, errors.Wrapf(err, "os.ReadDir(): %s", opts.Dir)
	}
	status, err := gitcmd.Output(ctx, opts.Root, "status", "--porcelain", "--", opts.Dir)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(status) != "" {
		return nil, errors.Newf("%s is not as %s holds it (git status lists a change under it): a consolidation folds the committed sequence alone, in a pull request of its own; commit or stash the change, or cut the branch from %s", opts.Dir, branch, branch)
	}
	committed, err := treeFiles(ctx, opts.Root, ref, opts.Dir)
	if err != nil {
		return nil, err
	}
	var seq []Entry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if NameRE.MatchString(e.Name()) && !committed[e.Name()] {
			return nil, errors.Newf("%s holds %s, which %s does not: a consolidation folds the branch's committed sequence alone, in a pull request of its own; merge the migration first, then consolidate on a branch cut from %s", opts.Dir, e.Name(), branch, branch)
		}
		seq = append(seq, Entry{Name: e.Name()})
	}
	problems, span := Sequence(seq)
	if len(problems) > 0 {
		return nil, errors.Newf("%s is not one sequence (%d problem(s); bedrock migration check lists them): a consolidation folds a sound sequence", opts.Dir, len(problems))
	}
	if span.Count < 2 {
		return c, nil
	}
	stems := ordered(withUp(readStems(entries, func(string) bool { return true })))
	c.Index = span.High
	if err := c.write(ctx, opts.Root, abs, stems); err != nil {
		return nil, err
	}

	return c, nil
}

// write folds the stems' files into the two new ones, writes them, removes the folded
// files and stages the change. The folded files go after the new ones are written, so
// the directory never empties; a folded file the new one replaces by name (a sequence
// whose last file was named Consolidated by hand) is rewritten, not removed.
func (c *Consolidation) write(ctx context.Context, root, abs string, stems []*stem) error {
	up, down, err := c.fold(abs, stems)
	if err != nil {
		return err
	}
	c.Up = fmt.Sprintf("%06d_%s.%s.sql", c.Index, ConsolidatedName, upKind)
	added := []string{path.Join(c.Dir, c.Up)}
	if err := os.WriteFile(filepath.Join(abs, c.Up), []byte(up), 0o600); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	if down != "" {
		c.Down = fmt.Sprintf("%06d_%s.%s.sql", c.Index, ConsolidatedName, downKind)
		added = append(added, path.Join(c.Dir, c.Down))
		if err := os.WriteFile(filepath.Join(abs, c.Down), []byte(down), 0o600); err != nil {
			return errors.Wrap(err, "os.WriteFile()")
		}
	}
	var removed []string
	for _, s := range stems {
		c.Folded = append(c.Folded, s.key())
		for _, kind := range s.files() {
			if name := s.key() + "." + kind + ".sql"; name != c.Up && name != c.Down {
				removed = append(removed, path.Join(c.Dir, name))
			}
		}
	}
	if _, err := gitcmd.Output(ctx, root, append([]string{"rm", "-q", "--"}, removed...)...); err != nil {
		return err
	}
	if _, err := gitcmd.Output(ctx, root, append([]string{"add", "--"}, added...)...); err != nil {
		return err
	}

	return nil
}

// fold reads the stems' files and writes the two contents: the up files in order, the
// down files in reverse order, each file's content under a comment naming it and closed
// with a semicolon where its last statement has none (the runner drops an empty
// statement, so a second semicolon would be harmless; one is tidier). The down content is
// empty when no stem has a down file.
func (c *Consolidation) fold(abs string, stems []*stem) (up, down string, err error) {
	var ups, downs strings.Builder
	fmt.Fprintf(&ups, "-- %06d_%s: the migrations %06d to %06d folded into one, identical by construction\n-- (bedrock migration consolidate); each file's statements follow a comment naming it.\n", c.Index, ConsolidatedName, stems[0].idx, c.Index)
	for _, s := range stems {
		if err := appendFile(&ups, abs, s.key()+"."+upKind+".sql"); err != nil {
			return "", "", err
		}
	}
	var withDown []*stem
	for _, s := range stems {
		if s.down {
			withDown = append(withDown, s)
		} else {
			c.WithoutDown = append(c.WithoutDown, s.key())
		}
	}
	if len(withDown) == 0 {
		return ups.String(), "", nil
	}
	fmt.Fprintf(&downs, "-- %06d_%s: the down files of the migrations %06d to %06d in reverse order\n-- (bedrock migration consolidate); each file's statements follow a comment naming it.\n", c.Index, ConsolidatedName, stems[0].idx, c.Index)
	for i := len(withDown) - 1; i >= 0; i-- {
		if err := appendFile(&downs, abs, withDown[i].key()+"."+downKind+".sql"); err != nil {
			return "", "", err
		}
	}

	return ups.String(), downs.String(), nil
}

// appendFile appends one file's content under a comment naming it, closed with a
// semicolon where its last statement has none.
func appendFile(b *strings.Builder, abs, name string) error {
	data, err := os.ReadFile(filepath.Join(abs, name))
	if err != nil {
		return errors.Wrap(err, "os.ReadFile()")
	}
	content := strings.TrimRight(string(data), " \t\r\n")
	fmt.Fprintf(b, "\n-- %s\n", name)
	if content == "" {
		return nil
	}
	b.WriteString(content)
	b.WriteString("\n")
	if needsSemicolon(content) {
		b.WriteString(";\n")
	}

	return nil
}

// needsSemicolon says whether the content's last statement is open: its last line that
// is not blank and not a line comment ends with no semicolon. Content that is comments
// alone holds no statement.
func needsSemicolon(content string) bool {
	lines := strings.Split(content, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}

		return !strings.HasSuffix(line, ";")
	}

	return false
}
