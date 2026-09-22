// Package advise writes the brief impulse advise hands to an agent: what the application's
// generate programs raised under -audit (or where the accepted warnings are pinned, when
// generation is skipped), where the accepted role warnings are pinned, impulse's own
// deterministic reading of each kind, and the question each kind leaves to judgment. The
// agent answers in prose and edits nothing; impulse reads no test file itself.
//
// Analysis by an agent is opt-in, a command of its own beside impulse audit: the check
// gates every change, the audit is read by decision, and advice is asked for.
package advise

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/audit"
	"github.com/cccteam/ccc/impulse/internal/check"
)

// File is the root-relative path of the brief. It is transient like the handoff's:
// written by the command, removed after an agent run, and never committed.
const File = ".impulse-advice.md"

// warningsTest is the file a program in the runner shape pins its accepted warnings in,
// beside the program.
const warningsTest = "warnings_test.go"

// Brief is what the agent is told.
type Brief struct {
	App *app.App
	// Options is the options check's result: the option set in force, when it passed.
	Options check.Result
	// Programs are the generate programs with what each raised, or, under SkipGenerate,
	// with the test that pins their accepted warnings.
	Programs []Program
	// SkipGenerate reports that no program was run: the agent reads the pinned warnings.
	SkipGenerate bool
	// RoleTest is the root-relative test that pins the accepted role warnings, and
	// RoleTestPresent whether it exists.
	RoleTest        string
	RoleTestPresent bool
}

// Program is one generate program in the brief.
type Program struct {
	// File is the program's declaring file, root-relative; Dir the directory it runs from.
	File string
	Dir  string
	// Lines are the Warning: and Audit: lines it raised, when it was run.
	Lines []string
	// WarningsTest is the root-relative test pinning its accepted warnings, and
	// WarningsTestPresent whether it exists.
	WarningsTest        string
	WarningsTestPresent bool
}

// Collect gathers the brief's facts: the option set, each program's lines under -audit
// (or, when skipGenerate, the test that pins them), and the role warnings test. A program
// that fails, or does not take -audit, is an error: the brief would miss its input, and
// the message is impulse audit's.
func Collect(ctx context.Context, env *check.Env, skipGenerate bool) (*Brief, error) {
	a := env.App
	if len(a.Generators) == 0 {
		return nil, fmt.Errorf("no generator program found (no file calls generation.NewResourceGenerator)")
	}
	b := &Brief{App: a, SkipGenerate: skipGenerate, RoleTest: check.ValidationTestFile(a)}
	b.RoleTestPresent = exists(a, b.RoleTest)
	options, err := check.Select([]string{"options"})
	if err != nil {
		return nil, err
	}
	b.Options = check.Run(ctx, env, options)[0]

	var ran []audit.Program
	if !skipGenerate {
		if env.Out != nil {
			fmt.Fprintln(env.Out, "running each generate program with -audit (needs the Spanner emulator)")
		}
		ran = audit.Collect(ctx, a, env.Exec)
	}
	for i, g := range a.Generators {
		p := Program{File: g.File, Dir: a.ProgramDir(g)}
		p.WarningsTest, p.WarningsTestPresent = warningsTestOf(a, g)
		if !skipGenerate {
			switch r := ran[i]; {
			case r.NoAudit:
				return nil, fmt.Errorf("%s: the program does not take -audit; adopt the runner shape (README, impulse audit), or pass --skip-generate to read its pinned warnings instead", r.File)
			case r.Failed:
				return nil, fmt.Errorf("%s: %s failed:\n%s", r.File, r.Command(), strings.Join(r.Tail, "\n"))
			default:
				p.Lines = r.Lines
			}
		}
		b.Programs = append(b.Programs, p)
	}

	return b, nil
}

// warningsTestOf names the test that pins a program's accepted warnings: beside the file
// declaring the generator (the skeletons keep the declaration, the runner, and the test
// in one directory; an application that declares in one package and runs it from a main
// package beside keeps the test with the declaration), or beside the runner, whichever
// exists; when neither does, the declaration's, as the file to write.
func warningsTestOf(a *app.App, g *app.Generator) (rel string, present bool) {
	declaring := path.Join(path.Dir(g.File), warningsTest)
	for _, candidate := range []string{declaring, path.Join(a.ProgramDir(g), warningsTest)} {
		if exists(a, candidate) {
			return candidate, true
		}
	}

	return declaring, false
}

func exists(a *app.App, rel string) bool {
	_, err := os.Stat(a.Abs(rel))

	return err == nil
}

// Warnings counts the Warning: lines across the programs, and Findings the Audit: lines.
func (b *Brief) Warnings() int { return b.count(audit.WarningPrefix) }

// Findings counts the Audit: lines across the programs.
func (b *Brief) Findings() int { return b.count(audit.AuditPrefix) }

func (b *Brief) count(prefix string) int {
	n := 0
	for i := range b.Programs {
		for _, line := range b.Programs[i].Lines {
			if strings.HasPrefix(line, prefix) {
				n++
			}
		}
	}

	return n
}

// Write renders the brief as Markdown.
func (b *Brief) Write(w io.Writer) {
	fmt.Fprintf(w, "# Impulse advice: %s\n\n", b.appName())
	fmt.Fprintf(w, "You are reading an Impulse application (Go services built on the cccteam libraries, with ccc/resource generating the handlers, routes, and browser clients from annotated resource structs). The application root is `%s`, and it is your working directory. impulse gathered what the application's schema and roles raised and reads each kind the same way every time; what no rule decides is the judgment each kind leaves to the application, and that is what you are asked for. Answer in prose. Edit nothing.\n\n", b.App.Root)

	if b.Options.Status == check.Pass {
		fmt.Fprintf(w, "## The option set in force\n\n%s\n", b.Options.Summary)
		for _, d := range b.Options.Details {
			fmt.Fprintf(w, "- %s\n", d)
		}
		fmt.Fprintf(w, "\n")
	}

	fmt.Fprintf(w, "## What the application raised\n\n")
	if b.SkipGenerate {
		fmt.Fprintf(w, "Generation was skipped, so the programs were not run. Each program pins the warnings it has accepted as typed values in its warnings test; read them there. An audit finding is never pinned: it is printed only when a program runs with `-audit`, so none is listed here.\n\n")
	} else {
		fmt.Fprintf(w, "Each generate program was run with `-audit`. A `Warning:` line is a schema warning the program raises on every run; an `Audit:` line is an advisory finding it raises only when asked.\n\n")
	}
	for i := range b.Programs {
		p := &b.Programs[i]
		fmt.Fprintf(w, "### %s (`go run ./%s -audit`)\n\n", p.File, p.Dir)
		switch {
		case b.SkipGenerate:
			fmt.Fprintf(w, "Accepted warnings: %s\n\n", b.pointer(p.WarningsTest, p.WarningsTestPresent, "the program is not in the runner shape (no warnings test beside it); run `impulse advise` without `--skip-generate` to read what it raises"))
		case len(p.Lines) == 0:
			fmt.Fprintf(w, "No warnings or findings.\n\n")
		default:
			for _, line := range p.Lines {
				fmt.Fprintf(w, "- %s\n", line)
			}
			fmt.Fprintf(w, "\nThe accepted set is pinned in %s.\n\n", b.pointer(p.WarningsTest, p.WarningsTestPresent, "no warnings test beside the program; a warning above is accepted nowhere in code yet"))
		}
	}

	fmt.Fprintf(w, "## Accepted role warnings\n\n")
	fmt.Fprintf(w, "The deploy-time validation of the roles files (`access.ValidateRoles`) prints a warning per grant it provisions as written but flags. The application accepts one by pinning its typed value in %s; each row of that test is one roles file, and an empty expected set means nothing is accepted. Read the pinned values there: they are role warnings to advise on too.\n\n",
		b.pointer(b.RoleTest, b.RoleTestPresent, "no such test exists yet, so no role warning is accepted in code; `impulse check` (auths-wired) says where it belongs"))

	fmt.Fprintf(w, "## How impulse reads each kind\n\n")
	fmt.Fprintf(w, "A line's kind is told by its wording. The reading is impulse's and fixed; the question is yours.\n\n")
	for _, k := range kinds {
		fmt.Fprintf(w, "**%s** (%s). %s\n\n*Question:* %s\n\n*Edits on offer:* %s\n\n", k.name, k.recognize, k.reading, k.question, k.edits)
	}
	fmt.Fprintf(w, "A line of a kind not listed here is read as written.\n\n")

	fmt.Fprintf(w, "## Rules\n\n")
	for i, rule := range rules {
		fmt.Fprintf(w, "%d. %s\n", i+1, rule)
	}
	fmt.Fprintf(w, "\n## Done when\n\nYour answer is written. There is nothing to run and nothing to verify: nothing was to be changed.\n")
}

// String renders the brief as Markdown.
func (b *Brief) String() string {
	var sb strings.Builder
	b.Write(&sb)

	return sb.String()
}

func (b *Brief) appName() string {
	if b.App.GoMod != nil && b.App.GoMod.Module != nil {
		return b.App.GoMod.Module.Mod.Path
	}

	return b.App.Root
}

// pointer names a file the agent reads, or says why it cannot.
func (b *Brief) pointer(rel string, present bool, absent string) string {
	if present {
		return "`" + rel + "`"
	}

	return "`" + rel + "` (absent: " + absent + ")"
}

// kind is impulse's reading of one warning or finding kind, with how a line of it reads,
// the question it leaves to judgment, and the edits that answer it.
type kind struct {
	name      string
	recognize string
	reading   string
	question  string
	edits     string
}

// kinds are the warning and finding kinds the framework raises today: the generator's
// three schema warnings, the audit pass's one finding, and the deploy-time validation's
// two role warnings.
var kinds = []kind{
	{
		name:      "Index warning",
		recognize: "`Warning: <Resource> lists in <columns> order with no index leading with those columns ...; wanted: CREATE INDEX ...`",
		reading:   "A listed tenant-scoped resource declares an `@order` that no index serves, so Spanner drives every page of its list from the tenant column's foreign-key index and sorts the tenant's whole partition each time. The line carries the `CREATE INDEX` the resource wants, verbatim; a migration adding it removes the warning, and nothing else in the application changes.",
		question:  "Whether the list pages at volume. A table that stays at tens of rows per tenant never feels the sort; a table that grows does, on every page, and the index is cheapest to add while the table is small.",
		edits:     "the migration with the `CREATE INDEX` from the line; or the pinned acceptance in the program's warnings test for a table that stays small, said so in its doc.",
	},
	{
		name:      "Join-path warning",
		recognize: "`Warning: <Resource> resolves its tenant through <column, table.column, ...>, so its lists scan all of <Table>, every tenant ...`",
		reading:   "A listed resource finds its tenant through `@domain(via: ...)`, a path of foreign keys, so no column on its own row names the tenant: every list scans the whole table across every tenant with a lookup per row up the path, and no index on the table changes that. The way out is a tenant column on the row (`@domain` on it), which is a schema change: a column, a backfill, and the annotation.",
		question:  "Whether the table will grow, and whether the tenant-column migration is worth doing now, while the table is small and the backfill is trivial, or the scan is acceptable for the table's expected size.",
		edits:     "the tenant column and its backfill migration, with `@domain` moved onto it; or the pinned acceptance for a table that stays small.",
	},
	{
		name:      "Enumeration size warning",
		recognize: "`Warning: <Type> enumerates <n> rows of <Table>, above the <limit> a baked table may hold ...`",
		reading:   "An `@enumerate` table is baked into the TypeScript metadata of every field keyed into it, one copy per field; the line gives the rows, the bytes, and the fields. Above the limit the browser carries that on every load. The alternative is a runtime resource: drop `@enumerate` from the type (the generated constants go with it) and expose the table with a `@resource` struct, whose picker reads it whole or paged under a `@page` maximum.",
		question:  "Whether the table is really an enumeration, a fixed short list the code names by constant, or a reference table that grows and should be a resource with a picker.",
		edits:     "the `@resource` struct and the `@enumerate` removal (and the code that used its constants); or the pinned acceptance for a list that is fixed and merely long.",
	},
	{
		name:      "Cascade release finding",
		recognize: "`Audit: <Resource> stores files on <Table>, whose rows the database deletes by cascade ...`",
		reading:   "A resource stores files on a table whose rows the database deletes by cascade (`ON DELETE CASCADE` on an interleave or a foreign key). Those rows never pass through the patch machinery, so the release of their objects at commit never runs for them; the application's sweep removes the objects later. An application that cares deletes the rows by patch before deleting the parent.",
		question:  "Whether that is acceptable for this table: how long objects may linger after their rows are gone, what they cost meanwhile, and whether anything reads them in between.",
		edits:     "a delete-by-patch of the child rows before the parent in the code path that deletes parents; or the schema change that drops the cascade; or acceptance, which nothing pins, said so in the resource's doc.",
	},
	{
		name:      "Grant warning (a role warning, pinned in the deploy test)",
		recognize: "`role <Role>: <Permission> on <Resource> is granted under \"<condition>\" without Read or List on <Row> ...`",
		reading:   "A role holds a conditional Delete, Update, or targeted Execute on a row it can neither Read nor List. A caller holding the role learns from a Forbidden answer that the row exists, where a read would have answered NotFound. Granting Read or List on the row resource in this role, or in a role assigned with it, closes it; a Read whose condition is narrower than the write's leaks the same way.",
		question:  "Whether the disclosure is intended: whether the row's existence is sensitive to this role, or the role is a writer who knows the rows anyway.",
		edits:     "a Read or List grant on the row resource in the roles file, with a condition at least as wide as the write's; or the pinned `access.GrantWarning` in the deploy test.",
	},
	{
		name:      "Concealing key warning (a role warning, pinned in the deploy test)",
		recognize: "`role <Role>: List on <Resource>.<field> is granted under \"<condition>\", and <field> is the default order|a sort or filter key of <Resource> whose masked cells conceal ...`",
		reading:   "A role lists a field under a condition, the field's masked cells conceal, and the role's other List grants on the resource leave the field's condition standing in the query, so a page ordered or filtered by the field sorts the tenant's whole partition on a CASE no index serves. Read from the line: whether the field is `the default order` (every page this role lists pays) or `a sort or filter key` (only a caller who asks for that sort or filter pays); and why the CASE stands, because the role `lists other fields unconditionally` or `also lists fields under` conditions the field's own does not cover. A table that never pages at volume does not care.",
		question:  "Whether the field's rank is sensitive (whether disclosing where a hidden value sorts gives the role something it should not have), and whether the table will page at volume for this role.",
		edits:     "the field granted unconditionally in this role, when its values are not sensitive to the role; `masking:\"positional\"` on the field, disclosing where hidden values fall in an order, when the rank is not; or the pinned `access.ConcealingKeyWarning` in the deploy test for a table that never pages at volume.",
	},
}

// rules are the agent's rules: an answer, no edit, no write.
var rules = []string{
	"Answer per warning and finding, in prose: name it, apply the reading above to it (read the resource struct, its migrations, the roles file, and how the table is used, to say which branch of the reading holds), answer the question from what the code tells you, and name the edit you recommend, from the edits on offer or better, or say the warning should be accepted and where its pin goes (the program's warnings test for a schema warning, the deploy test for a role warning). The pinned values in those tests are warnings to advise on too.",
	"Edit nothing. Run nothing that writes: no `go generate`, no `impulse` command, no test. Read the code.",
	"Where the code cannot answer the question (whether a table will grow, whether a field's rank is sensitive to a role), say so and state the question for the developer in one sentence.",
	"Finish with the edits you recommend as a list, in the order you would make them, each naming its file.",
}
