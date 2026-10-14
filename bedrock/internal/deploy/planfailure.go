package deploy

import (
	"bufio"
	"strconv"
	"strings"
)

// failedPlanErrors is how many of a failed plan's errors the pull request comment carries;
// the rest are counted, and the build log has them.
const failedPlanErrors = 3

// planErrors reads the errors off a failed plan's text output, with color off: the
// first line of each "Error:" block, which tofu renders as the error's summary and, for
// a provider's error, the message in it.
func planErrors(text string) []string {
	var errs []string
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if rest, ok := strings.CutPrefix(line, "Error: "); ok && rest != "" {
			errs = append(errs, rest)
		}
	}

	return errs
}

// failedPlanLine is the comment line for an environment whose plan failed: the plan's
// errors (planErrors), the first failedPlanErrors of them when there are more, and the
// build that has the whole log; the fixed line when the output recorded no error.
func failedPlanLine(env, text, buildID string) string {
	errs := planErrors(text)
	if len(errs) == 0 {
		return "The plan of " + env + "'s stack failed; the build log says why (build " + buildID + ")."
	}
	shown := make([]string, 0, failedPlanErrors)
	for _, e := range errs[:min(len(errs), failedPlanErrors)] {
		shown = append(shown, "`"+e+"`")
	}
	line := "The plan of " + env + "'s stack failed: " + strings.Join(shown, "; ")
	if more := len(errs) - failedPlanErrors; more > 0 {
		line += "; and " + strconv.Itoa(more) + " more"
	}

	return line + " (build " + buildID + " has the log)."
}
