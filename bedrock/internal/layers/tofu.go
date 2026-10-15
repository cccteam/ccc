// tofu.go drives tofu in one layer's directory as the layer's identity: the init at the
// run's state prefix, the plan with its JSON log, the output changes a plan makes, and
// the apply of a saved plan.

package layers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// The files a run leaves in its working directory: the saved plan and tofu's JSON log of
// the plan, from which the errors are read.
const (
	planFile = "plan.tfplan"
	logFile  = "plan-log.json"
	// identityEnv is the variable the Google provider and the state backend impersonate
	// the layer's identity through; githubEnv the one 1-org's GitHub provider reads its
	// installation token from; dataDirEnv keeps one backend cache per environment of
	// 2-env in the one directory.
	identityEnv = "GOOGLE_IMPERSONATE_SERVICE_ACCOUNT"
	githubEnv   = "GITHUB_TOKEN"
	dataDirEnv  = "TF_DATA_DIR"
	// diagnosticType is the type of a log line that carries an error or a warning.
	diagnosticType = "diagnostic"
	// The flags every tofu command gets: no questions, no color.
	noInput = "-input=false"
	noColor = "-no-color"
)

// tofu is one run's tofu: the layer's directory, the environment the run gets, the
// working directory the plan goes to, and where its output goes.
type tofu struct {
	exec     deploy.Runner
	run      org.LayerRun
	identity string
	dir      string
	work     string
	env      []string
	out      io.Writer
}

// newTofu sets a run up in the layer's directory as the identity, with the working
// directory under the system's temporary files, and the GitHub token for 1-org.
func newTofu(exec deploy.Runner, root string, lr org.LayerRun, identity, githubToken string, out io.Writer) (*tofu, error) {
	work, err := os.MkdirTemp("", "bedrock-"+lr.Layer+"-")
	if err != nil {
		return nil, errors.Wrap(err, "os.MkdirTemp()")
	}
	env := []string{identityEnv + "=" + identity}
	if lr.Environment != "" {
		env = append(env, dataDirEnv+"=.terraform."+lr.Environment)
	}
	if lr.Layer == orgLayer {
		env = append(env, githubEnv+"="+githubToken)
	}

	return &tofu{exec: exec, run: lr, identity: identity, dir: filepath.Join(root, lr.Layer), work: work, env: env, out: out}, nil
}

// close removes the working directory.
func (t *tofu) close() {
	_ = os.RemoveAll(t.work)
}

// command is one tofu command in the layer's directory.
func (t *tofu) command(args ...string) deploy.Command {
	return deploy.Command{Dir: t.dir, Env: t.env, Name: "tofu", Args: args}
}

// init points the layer at the run's state prefix, impersonating the identity for the
// backend as for the provider.
func (t *tofu) init(ctx context.Context) error {
	return t.exec.Run(ctx, t.command("init", noInput, noColor, "-backend-config=prefix="+t.run.StatePrefix, "-backend-config=impersonate_service_account="+t.identity), t.out)
}

// plan plans the layer into the working directory, with the JSON log beside it, and
// answers what tofu printed and whether it succeeded; the plan runs without the state
// lock on a pull request (the plan identity reads and writes nothing). An error is the
// run's own, never the plan failing.
func (t *tofu) plan(ctx context.Context, lock bool) (text string, ok bool) {
	args := []string{"plan", noInput, noColor, "-out=" + filepath.Join(t.work, planFile), "-json-into=" + filepath.Join(t.work, logFile)}
	if !lock {
		args = append(args, "-lock=false")
	}
	if t.run.Environment != "" {
		args = append(args, "-var", "environment="+t.run.Environment)
	}
	var printed bytes.Buffer
	err := t.exec.Run(ctx, t.command(args...), io.MultiWriter(&printed, t.out))

	return printed.String(), err == nil
}

// apply applies the saved plan and answers what tofu printed.
func (t *tofu) apply(ctx context.Context) (string, error) {
	var printed bytes.Buffer
	if err := t.exec.Run(ctx, t.command(applyCommand, noInput, noColor, filepath.Join(t.work, planFile)), io.MultiWriter(&printed, t.out)); err != nil {
		return printed.String(), err
	}

	return printed.String(), nil
}

// diagnostic is one error tofu's JSON log carries.
type diagnostic struct {
	Summary string `json:"summary"`
	Detail  string `json:"detail"`
}

// errorDiagnostics reads the plan's errors off its JSON log: each line of type
// diagnostic whose severity is error. No log (the plan did not get that far) is no
// errors.
func (t *tofu) errorDiagnostics() ([]diagnostic, error) {
	data, err := os.ReadFile(filepath.Join(t.work, logFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, errors.Wrap(err, "os.ReadFile(): the plan's log")
	}
	var diagnostics []diagnostic
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry struct {
			Type       string `json:"type"`
			Diagnostic struct {
				Severity string `json:"severity"`
				Summary  string `json:"summary"`
				Detail   string `json:"detail"`
			} `json:"diagnostic"`
		}
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		if entry.Type == diagnosticType && entry.Diagnostic.Severity == "error" {
			diagnostics = append(diagnostics, diagnostic{Summary: entry.Diagnostic.Summary, Detail: entry.Diagnostic.Detail})
		}
	}

	return diagnostics, nil
}

// outputChanges reads, off the saved plan, the output changes it makes: each output
// with the actions on it (create, update, delete, no-op), without its value.
func (t *tofu) outputChanges(ctx context.Context) (map[string][]string, error) {
	shown, err := t.exec.Output(ctx, t.command("show", "-json", filepath.Join(t.work, planFile)), t.out)
	if err != nil {
		return nil, err
	}
	var plan map[string]json.RawMessage
	if err := json.Unmarshal(shown, &plan); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal(): tofu show -json")
	}
	var outputs map[string]struct {
		Actions []string `json:"actions"`
	}
	if raw, ok := plan["output_changes"]; ok {
		if err := json.Unmarshal(raw, &outputs); err != nil {
			return nil, errors.Wrap(err, "json.Unmarshal(): tofu show -json, output_changes")
		}
	}
	changes := make(map[string][]string, len(outputs))
	for name, change := range outputs {
		changes[name] = change.Actions
	}

	return changes, nil
}
