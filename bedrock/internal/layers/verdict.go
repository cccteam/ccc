// verdict.go judges one run's plan of a pull request once every plan has finished: a
// plan that succeeded passes with its summary; a plan that failed only because it reads
// outputs the plans of runs before it in layer order create passes too, planned after
// those runs apply, since the merge applies them first; a run with no state yet passes,
// planned after its first apply; any other failure fails, a plan that did not run
// included.

package layers

import (
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/cccteam/ccc/bedrock/internal/org"
)

// The verdict's fixed lines, the summary of the error a read of a missing output gives,
// and the action a plan shows for an output it creates. A failed plan's line carries
// its errors, up to failedErrors of them.
const (
	planFailed     = "The plan failed; the log says why."
	planFailedWith = "The plan failed: "
	planNotRun     = "The plan did not run; the log says why."
	firstApply     = "planned after its first apply: the run has no state yet (it has never applied), so nothing was planned, and the merge applies it."
	outputsOnly    = "No resource changes; outputs change only."
	noSummary      = "no summary line"
	unsupportedSum = "Unsupported attribute"
	createAction   = "create"
	failedErrors   = 3
)

var (
	// summaryRE is the line tofu plan ends with; outputsRE the heading a plan that
	// changes outputs alone prints instead.
	summaryRE = regexp.MustCompile(`(?m)^(Plan:.*|No changes\..*)$`)
	outputsRE = regexp.MustCompile(`(?m)^Changes to Outputs:`)
	// lacksRE is the detail of an object read without the attribute named.
	lacksRE = regexp.MustCompile(`^This object does not have an attribute named "([^"]+)"\.$`)
)

// planResult is one run's plan as the verdict judges it.
type planResult struct {
	run org.LayerRun
	// unapplied reports that the run has no state yet and nothing was planned; ran
	// whether tofu plan ran at all (the init succeeded); ok whether it succeeded.
	unapplied bool
	ran       bool
	ok        bool
	// text is what tofu plan printed; diagnostics its errors; outputs the output
	// changes it makes, when it succeeded.
	text        string
	diagnostics []diagnostic
	outputs     map[string][]string
	// log is everything the run printed, for the build's log; err is a failure of the
	// run itself (its working directory, its log), beside the plan's.
	log string
	err error
}

// verdict is one plan's judgement: its line for the pull request, and whether it
// passes.
type verdict struct {
	summary string
	pass    bool
}

// verdicts judges every plan, in the order the merge applies the runs.
func verdicts(results []*planResult) []verdict {
	out := make([]verdict, 0, len(results))
	for i, res := range results {
		out = append(out, judge(res, results[:i]))
	}

	return out
}

// judge is one plan's verdict, given the results of the runs before it.
func judge(res *planResult, earlier []*planResult) verdict {
	switch {
	case res.err != nil:
		return verdict{summary: planNotRun}
	case res.unapplied:
		return verdict{summary: firstApply, pass: true}
	case !res.ran:
		return verdict{summary: planNotRun}
	case res.ok:
		return verdict{summary: summaryOf(res.text), pass: true}
	}
	attributes, ok := lackingAttributes(res.diagnostics)
	if !ok {
		return verdict{summary: failedSummary(res.diagnostics)}
	}
	producers, byProducer := producersOf(attributes, earlier)
	if producers == nil {
		return verdict{summary: failedSummary(res.diagnostics)}
	}

	return verdict{summary: plannedAfter(producers, byProducer), pass: true}
}

// failedSummary is a failed plan's line: its errors, each the summary and the first
// line of the detail tofu gave, the first failedErrors of them when there are more;
// the fixed line when it recorded none.
func failedSummary(diagnostics []diagnostic) string {
	if len(diagnostics) == 0 {
		return planFailed
	}
	var errs []string
	for _, d := range diagnostics[:min(len(diagnostics), failedErrors)] {
		text := strings.TrimSpace(d.Summary)
		if detail, _, _ := strings.Cut(strings.TrimSpace(d.Detail), "\n"); detail != "" {
			text += ": " + detail
		}
		errs = append(errs, "`"+text+"`")
	}
	line := planFailedWith + strings.Join(errs, "; ")
	if more := len(diagnostics) - failedErrors; more > 0 {
		line += "; and " + strconv.Itoa(more) + " more, which the log has"
	}

	return line + "."
}

// summaryOf is a succeeded plan's last Plan: or No changes. line, the outputs-only
// sentence when the plan changes outputs alone, else that there is no summary line.
func summaryOf(text string) string {
	lines := summaryRE.FindAllString(text, -1)
	if len(lines) > 0 {
		return strings.TrimSpace(lines[len(lines)-1])
	}
	if outputsRE.MatchString(text) {
		return outputsOnly
	}

	return noSummary
}

// lackingAttributes is the attributes the plan's errors name, sorted and unique, when
// every error is an object read without the attribute named; false when any error is
// something else, or there is none.
func lackingAttributes(diagnostics []diagnostic) ([]string, bool) {
	seen := map[string]bool{}
	for _, d := range diagnostics {
		m := lacksRE.FindStringSubmatch(d.Detail)
		if d.Summary != unsupportedSum || m == nil {
			return nil, false
		}
		seen[m[1]] = true
	}
	if len(seen) == 0 {
		return nil, false
	}
	attributes := make([]string, 0, len(seen))
	for a := range seen {
		attributes = append(attributes, a)
	}
	sort.Strings(attributes)

	return attributes, true
}

// producersOf finds, for each attribute, the first earlier run whose plan creates an
// output of that name; nil when one has none. It answers the producers in the order
// they were first found and the attributes each one creates.
func producersOf(attributes []string, earlier []*planResult) (producers []string, byProducer map[string][]string) {
	byProducer = map[string][]string{}
	for _, attribute := range attributes {
		producer := ""
		for _, res := range earlier {
			if res.ok && slices.Equal(res.outputs[attribute], []string{createAction}) {
				producer = res.run.Name

				break
			}
		}
		if producer == "" {
			return nil, nil
		}
		if _, ok := byProducer[producer]; !ok {
			producers = append(producers, producer)
		}
		byProducer[producer] = append(byProducer[producer], attribute)
	}

	return producers, byProducer
}

// plannedAfter is the verdict's line for a plan that reads outputs the earlier plans
// create.
func plannedAfter(producers []string, byProducer map[string][]string) string {
	if len(producers) == 1 {
		p := producers[0]
		noun := "the output"
		if len(byProducer[p]) > 1 {
			noun = "the outputs"
		}

		return "planned after " + p + " applies: this plan reads " + noun + " " + joined(byProducer[p]) + ", which " + p + "'s plan creates, and the merge applies " + p + " first."
	}
	sources := make([]string, 0, len(producers))
	for _, p := range producers {
		sources = append(sources, joined(byProducer[p])+" from "+p)
	}

	return "planned after " + joined(producers) + " apply: this plan reads outputs their plans create (" + strings.Join(sources, "; ") + "), and the merge applies them first."
}
