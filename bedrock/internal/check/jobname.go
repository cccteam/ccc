// jobname.go checks the image build against the job process's contract: the site learns
// the job of its own build from the image (the pipeline passes JOBS_JOB to the build, and
// the Dockerfile sets it as the site's variable), and a Dockerfile that drops either line
// leaves the site naming no job, after the stack and the pipeline said nothing.

package check

import (
	"os"
	"path/filepath"
	"regexp"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// JobNameFinding is a Dockerfile that does not carry the build's job to the site.
type JobNameFinding struct {
	// Var is the site's variable naming the job (APP_JOBS_JOB), Missing the instruction
	// the Dockerfile lacks: ARG JOBS_JOB, or ENV <Var>="${JOBS_JOB}".
	Var     string
	Missing string
}

// The build argument the pipeline passes and the ENV that hands it to the site.
const jobsJobArg = "JOBS_JOB"

// scanJobName reads the Dockerfile for the two instructions that carry the build's job
// to the site, for an application with a job process whose site declares the job
// variable: an ARG JOBS_JOB in the runtime stage and an ENV setting the variable from
// it. An application without a Dockerfile has nothing to scan: the unseeded finding
// covers it.
func scanJobName(appDir string, m *derive.Model) ([]JobNameFinding, error) {
	if m.Jobs == nil {
		return nil, nil
	}
	var jobsVar *derive.Variable
	for _, v := range m.ByLevel(derive.LevelSite) {
		if v.Role == derive.RoleJobsJob {
			jobsVar = v
		}
	}
	if jobsVar == nil {
		return nil, nil
	}
	src, err := os.ReadFile(filepath.Join(appDir, dockerfileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, errors.Wrap(err, "os.ReadFile(): Dockerfile")
	}
	instructions := instructionLines(src)
	var findings []JobNameFinding
	if !regexp.MustCompile(`(?m)^ARG ` + jobsJobArg + `\b`).MatchString(instructions) {
		findings = append(findings, JobNameFinding{Var: jobsVar.Name, Missing: "ARG " + jobsJobArg})
	}
	if !regexp.MustCompile(`(?m)^ENV\b[^\n]*\b`+regexp.QuoteMeta(jobsVar.Name)+`="?\$\{?`+jobsJobArg+`\}?"?`).MatchString(instructions) && !regexp.MustCompile(`(?m)^\s*`+regexp.QuoteMeta(jobsVar.Name)+`="?\$\{?`+jobsJobArg+`\}?"?`).MatchString(instructions) {
		findings = append(findings, JobNameFinding{Var: jobsVar.Name, Missing: "ENV " + jobsVar.Name + "=\"${" + jobsJobArg + "}\""})
	}

	return findings, nil
}
