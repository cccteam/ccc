// Package handoff is the protocol between impulse and an agent for the work the tool
// cannot do mechanically. The tool lays in what files and AST edits can do reliably, runs
// impulse check, and the failing checks are exactly the obligations left. The brief
// written here hands those obligations to an agent verbatim, with the rules; the agent
// edits, regenerates, and re-runs the check until it is clean; and Verify then re-runs
// the check itself and compares the guardrails (the generator option set and the lint
// configuration) against the version control index, so a weakened check is a failure and
// not a pass. The gate after that is the pull request.
//
// The brief is a file the agent reads; the tool never reads it back. What the tool needs
// afterwards it reads from the tree and the index.
package handoff

import (
	"fmt"
	"io"
	"strings"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/check"
)

// File is the root-relative path of the brief. It is transient: written at handoff,
// removed when the verification passes, and never committed.
const File = ".impulse-handoff.md"

// CheckCommand is the check the agent runs until it is clean.
const CheckCommand = "impulse check"

// Brief is what the agent is told.
type Brief struct {
	App *app.App
	// Change says what the tool changed and why. Empty when the tool changed nothing and
	// the failing checks were found in the tree as it is.
	Change string
	// Meaning says what the option in question means in this framework. Optional.
	Meaning string
	// Results are the check results at the handoff. The failing ones are the obligations;
	// the options result states the option set the agent must leave in force.
	Results []check.Result
	// Reference is the path of a finished application with the same options wired, for
	// the shape. Optional.
	Reference string
	// Guard names the files the agent must not move.
	Guard Snapshot
}

// Write renders the brief as Markdown.
func (b *Brief) Write(w io.Writer) {
	fmt.Fprintf(w, "# Impulse handoff: %s\n\n", b.appName())
	fmt.Fprintf(w, "You are working in an Impulse application (Go services built on the cccteam libraries, with ccc/resource generating the handlers, routes, and browser clients from annotated resource structs). The application root is `%s`, and it is your working directory.\n\n", b.App.Root)

	fmt.Fprintf(w, "## What changed\n\n")
	if b.Change == "" {
		fmt.Fprintf(w, "Nothing was changed by the tool. `%s` found the obligations below in the tree as it is.\n\n", CheckCommand)
	} else {
		fmt.Fprintf(w, "%s\n\n", strings.TrimSpace(b.Change))
	}
	if b.Meaning != "" {
		fmt.Fprintf(w, "## What it means\n\n%s\n\n", strings.TrimSpace(b.Meaning))
	}

	if options, ok := b.result("options"); ok && options.Status == check.Pass {
		fmt.Fprintf(w, "## The option set in force\n\n%s\n", options.Summary)
		for _, d := range options.Details {
			fmt.Fprintf(w, "- %s\n", d)
		}
		fmt.Fprintf(w, "\nThis is read from the generator programs, never from a record. It must read the same when you are done.\n\n")
	}

	fmt.Fprintf(w, "## The failing checks\n\nThis is the output of `%s`, failing checks only. Each line under a check is one obligation.\n\n```\n", CheckCommand)
	check.Report(w, b.failing())
	fmt.Fprintf(w, "```\n\n")

	if b.Reference != "" {
		fmt.Fprintf(w, "## Reference\n\nThe application at `%s` has these options wired and its check clean. Read it for the shape of the wiring. Do not copy its resources, names, or data into this application.\n\n", b.Reference)
	}

	fmt.Fprintf(w, "## Rules\n\n")
	for i, rule := range b.rules() {
		fmt.Fprintf(w, "%d. %s\n", i+1, rule)
	}
	fmt.Fprintf(w, "\n## Done when\n\n")
	fmt.Fprintf(w, "`%s` reports no FAIL", CheckCommand)
	if options, ok := b.result("options"); ok && options.Status == check.Pass {
		fmt.Fprintf(w, ", and its options line still reads:\n\n    %s\n", options.Summary)
	} else {
		fmt.Fprintf(w, ".\n")
	}
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

func (b *Brief) result(name string) (check.Result, bool) {
	for _, r := range b.Results {
		if r.Name == name {
			return r, true
		}
	}

	return check.Result{}, false
}

func (b *Brief) failing() []check.Result {
	var failing []check.Result
	for _, r := range b.Results {
		if r.Status == check.Fail {
			failing = append(failing, r)
		}
	}

	return failing
}

// rules are the agent's rules. They are short, and every one of them is either verified
// by the check or by Verify's guardrail comparison.
func (b *Brief) rules() []string {
	rules := []string{
		fmt.Sprintf("Run `%s` from the application root until it reports no FAIL. The regen check runs go generate, which needs the Spanner emulator through podman or docker; if neither is available, run `%s --skip-generate` and say so in your final message.", CheckCommand, CheckCommand),
		"Do not edit generated files (`zz_gen_*`). Change the source they are generated from and run `go generate ./...`.",
	}
	if programs := b.Guard.programFiles(); len(programs) > 0 {
		rules = append(rules, fmt.Sprintf("Do not edit the generator program(s): %s. Do not add or remove generator options; the option set was recorded and is compared when you finish.", codeList(programs)))
	}
	if configs := b.Guard.configFiles(); len(configs) > 0 {
		rules = append(rules, fmt.Sprintf("Do not edit the lint configuration: %s. Fix the findings in the code.", codeList(configs)))
	}
	rules = append(rules,
		"Do not stage or commit. Leave your work in the working tree; the pull request is the review.",
		"Keep the tests table-driven and the suite passing: `go test ./...`.",
		"Stop when the check is clean. In your final message, say what you changed and what a reviewer should look at.",
	)

	return rules
}

func codeList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		quoted = append(quoted, "`"+item+"`")
	}

	return strings.Join(quoted, ", ")
}
