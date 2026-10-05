// Package deploy is the deploy sequence as commands: each step of the pipeline (the
// deployment record first; the others follow) over the same inputs, the facts the
// pipeline's resolve step writes to the workspace and the build's own substitutions, so
// a pipeline file composes them and an application with nothing custom lists them all.
package deploy

import (
	"bufio"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
)

// The files a build's steps share under the workspace.
const (
	// EnvironmentFile holds the facts the resolve step exports, one `export NAME=value`
	// per line, appended to by later steps (the image digest, the reuse flag).
	EnvironmentFile = "environment.sh"
	// BuildFile is the build as Cloud Build describes it (gcloud builds describe, JSON):
	// its id and every substitution.
	BuildFile = "build.json"
	// RevisionsFile lists the revisions the deploy step created, one `region,service,
	// revision` per line.
	RevisionsFile = "revisions.txt"
)

// Workspace is where a build's steps share their files: /workspace in Cloud Build.
type Workspace string

// Environment reads the facts from the workspace's environment file: the value of every
// `export NAME=value` line, unquoted the way a shell would read it (double quotes with
// the shell's escapes, single quotes with the '\” idiom, or bare). Other lines are
// ignored.
func (w Workspace) Environment() (map[string]string, error) {
	f, err := os.Open(filepath.Join(string(w), EnvironmentFile))
	if err != nil {
		return nil, errors.Wrap(err, "os.Open()")
	}
	defer f.Close()

	env := map[string]string{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "export ") {
			continue
		}
		name, raw, ok := strings.Cut(strings.TrimSpace(strings.TrimPrefix(line, "export ")), "=")
		if !ok {
			continue
		}
		value, err := unquote(raw)
		if err != nil {
			return nil, errors.Wrapf(err, "%s: %s", EnvironmentFile, name)
		}
		env[name] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.Wrap(err, "bufio.Scanner.Scan()")
	}

	return env, nil
}

// Append adds facts to the environment file, one export line each, sorted by name: a
// later step's findings (the image digest, the reuse flag) for the steps after it.
func (w Workspace) Append(facts map[string]string) error {
	f, err := os.OpenFile(filepath.Join(string(w), EnvironmentFile), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.Wrap(err, "os.OpenFile()")
	}
	defer f.Close()
	for _, name := range slices.Sorted(maps.Keys(facts)) {
		if _, err := f.WriteString("export " + name + "=" + doubleQuote(facts[name]) + "\n"); err != nil {
			return errors.Wrapf(err, "os.File.WriteString(): %s", EnvironmentFile)
		}
	}

	return nil
}

// unquote reads a shell word: single-quoted (a quote inside written as '\”),
// double-quoted (with \", \\, \$ and \` unescaped), or bare.
func unquote(raw string) (string, error) {
	switch {
	case raw == "":
		return "", nil
	case raw[0] == '\'':
		var b strings.Builder
		rest := raw
		for {
			if rest == "" || rest[0] != '\'' {
				return "", errors.Newf("unterminated single quote in %s", raw)
			}
			end := strings.IndexByte(rest[1:], '\'')
			if end < 0 {
				return "", errors.Newf("unterminated single quote in %s", raw)
			}
			b.WriteString(rest[1 : 1+end])
			rest = rest[2+end:]
			if strings.HasPrefix(rest, `\'`) {
				b.WriteByte('\'')
				rest = rest[2:]

				continue
			}
			if rest != "" {
				return "", errors.Newf("text after the closing quote in %s", raw)
			}

			return b.String(), nil
		}
	case raw[0] == '"':
		if len(raw) < 2 || raw[len(raw)-1] != '"' {
			return "", errors.Newf("unterminated double quote in %s", raw)
		}
		var b strings.Builder
		body := raw[1 : len(raw)-1]
		for i := 0; i < len(body); i++ {
			if body[i] == '\\' && i+1 < len(body) && strings.IndexByte("\"\\$`", body[i+1]) >= 0 {
				i++
			}
			b.WriteByte(body[i])
		}

		return b.String(), nil
	default:
		return raw, nil
	}
}

// Build is the build as Cloud Build describes it: its id and every substitution, the
// trigger's and Cloud Build's own (COMMIT_SHA, TAG_NAME) alike.
type Build struct {
	ID            string            `json:"id"`
	Substitutions map[string]string `json:"substitutions"`
	// StartTime is when the build started running (after its approval, where one is
	// needed) and Timeout its whole-build timeout (86400s), from which the wait for a
	// maintenance window knows how long the run may still take.
	StartTime string `json:"startTime,omitempty"`
	Timeout   string `json:"timeout,omitempty"`
	// Approval is the build's approval as Cloud Build describes it, where the trigger
	// requires one (placement.json's approvals): decided before the build ran, so the
	// run knows who approved it and writes them into the record. Nil where none is
	// required.
	Approval *BuildApproval `json:"approval,omitempty"`
}

// BuildApproval is a build's approval: its state (PENDING, APPROVED, REJECTED) and, once
// decided, the result.
type BuildApproval struct {
	State  string               `json:"state,omitempty"`
	Result *BuildApprovalResult `json:"result,omitempty"`
}

// BuildApprovalResult is who decided a build's approval, when, which way and with what
// comment, as Cloud Build records it.
type BuildApprovalResult struct {
	ApproverAccount string `json:"approverAccount,omitempty"`
	ApprovalTime    string `json:"approvalTime,omitempty"`
	Decision        string `json:"decision,omitempty"`
	Comment         string `json:"comment,omitempty"`
}

// approvedDecision is the decision of an approval that let the build run.
const approvedDecision = "APPROVED"

// approver is who approved the build and when, with their comment; empty where the build
// needed no approval.
func (b *Build) approver() (account, at, comment string) {
	if b.Approval == nil || b.Approval.Result == nil || b.Approval.Result.Decision != approvedDecision {
		return "", "", ""
	}

	return b.Approval.Result.ApproverAccount, b.Approval.Result.ApprovalTime, b.Approval.Result.Comment
}

// parseBuild reads a build as Cloud Build describes it.
func parseBuild(data []byte) (*Build, error) {
	var b Build
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal(): the build")
	}
	if b.Substitutions == nil {
		b.Substitutions = map[string]string{}
	}

	return &b, nil
}

// Build reads the workspace's build file.
func (w Workspace) Build() (*Build, error) {
	data, err := os.ReadFile(filepath.Join(string(w), BuildFile))
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	var b Build
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, errors.Wrapf(err, "json.Unmarshal(): %s", BuildFile)
	}
	if b.ID == "" {
		return nil, errors.Newf("%s names no build id", BuildFile)
	}

	return &b, nil
}

// Revision is one revision the deploy step created.
type Revision struct {
	Region   string `json:"region"`
	Service  string `json:"service"`
	Revision string `json:"revision"`
}

// WriteRevisions leaves the revisions the deploy step created: one region,service,revision
// per line.
func (w Workspace) WriteRevisions(revisions []Revision) error {
	var b strings.Builder
	for _, r := range revisions {
		b.WriteString(r.Region + "," + r.Service + "," + r.Revision + "\n")
	}
	if err := os.WriteFile(filepath.Join(string(w), RevisionsFile), []byte(b.String()), 0o600); err != nil {
		return errors.Wrapf(err, "os.WriteFile(): %s", RevisionsFile)
	}

	return nil
}

// Revisions reads the workspace's revisions file: one region,service,revision per line.
func (w Workspace) Revisions() ([]Revision, error) {
	data, err := os.ReadFile(filepath.Join(string(w), RevisionsFile))
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	var revisions []Revision
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) != 3 {
			return nil, errors.Newf("%s: %q is not region,service,revision", RevisionsFile, line)
		}
		revisions = append(revisions, Revision{Region: parts[0], Service: parts[1], Revision: parts[2]})
	}

	return revisions, nil
}
