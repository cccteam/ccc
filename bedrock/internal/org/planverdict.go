// planverdict.go holds the verdict on a pull request's plan of one layer run, which the
// layers workflow runs once every plan of the pull request has finished: a plan that
// fails only because it reads outputs an earlier run's plan creates is planned after that
// run applies, and passes.

package org

import (
	_ "embed"
	"strings"
)

// planVerdictScript is the verdict, a bash script over jq: the workflow writes it out and
// runs it in each run's plan job (planverdict.sh says what it reads and answers).
//
//go:embed planverdict.sh
var planVerdictScript string

// verdictIndent is the indentation of the plan job's run block, where the workflow writes
// the script out through a here-document.
const verdictIndent = "          "

// PlanVerdict is the verdict script as the workflow's run block carries it: every line
// indented to the block, empty lines left empty.
func (*view) PlanVerdict() string {
	lines := strings.Split(strings.TrimSuffix(planVerdictScript, "\n"), "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = verdictIndent + line
		}
	}

	return strings.Join(lines, "\n")
}
