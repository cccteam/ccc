// guardplan.go is the guard on a pull request's plan.

package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
)

// GuardPlan lets through only what belongs to the pull request. A pull request may have
// edited the stack any way at all, so every resource the saved plan creates, changes or
// destroys must carry the pull request's name (<app>-pr<N>) in what names it (name,
// account_id, service, job, database, parent, secret_id, service_account_id, bucket, or
// the source of a ruleset), or be an IAM membership of one of the pull request's own
// accounts. Resources outside the google
// provider (time_sleep) shape nothing and pass. Anything else stops the build and is
// listed on the pull request. Shared mode (/gcbrun shared-db) is refused too when the
// pull request changes the migrations, which would change the shared database before any
// release.
func GuardPlan(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
	env, build, ok, err := pullRequestStep(w, out)
	if err != nil || !ok {
		return err
	}
	subs := build.Substitutions
	if env[sharedDBFact] == trueValue {
		if err := sharedMigrations(ctx, clients, subs, env, out); err != nil {
			return err
		}
	}
	data, err := os.ReadFile(filepath.Join(string(w), PlanJSONFile))
	if err != nil {
		return errors.Wrap(err, "os.ReadFile()")
	}
	name := subs[appSub] + "-pr" + subs[prNumberSub]
	offenders, count, err := planOffenders(data, name)
	if err != nil {
		return err
	}
	if len(offenders) == 0 {
		fmt.Fprintf(out, "Guard passed: %d planned change(s), all pull request %s's.\n", count, subs[prNumberSub])

		return nil
	}
	list := "  " + strings.Join(offenders, "\n  ")
	fmt.Fprintf(out, "Build REJECTED: the plan touches resources that are not pull request %s's:\n%s\n", subs[prNumberSub], list)
	pr, err := speaker(ctx, clients, build, env, true)
	if err != nil {
		return err
	}
	if pr != nil {
		body := fmt.Sprintf("The pull-request build stopped: its plan touches resources that do not belong to this pull request. A pull-request stack applies only resources named %s, or memberships of its own accounts. Build %s:\n\n```\n%s\n```", name, build.ID, strings.Join(offenders, "\n"))
		if err := pr.comment(ctx, body, out); err != nil {
			return err
		}
	}

	return errors.Newf("%sthe plan touches resources that are not pull request %s's", rejected, subs[prNumberSub])
}

// sharedMigrations refuses shared mode for a pull request that changes the schema
// migrations against the default branch.
func sharedMigrations(ctx context.Context, clients *Clients, subs, env map[string]string, out io.Writer) error {
	owner, repo, err := splitRepo(subs[repoFullNameSub])
	if err != nil {
		return err
	}
	cmp, err := clients.GitHub(env[githubTokenFact]).Compare(ctx, owner, repo, subs[defaultBranchSub], subs[commitSub])
	if err != nil {
		return err
	}
	dir := strings.TrimSuffix(subs[migrationsSub], "/") + "/"
	var changed []string
	for _, f := range cmp.Files {
		if subs[migrationsSub] != "" && strings.HasPrefix(f.Filename, dir) {
			changed = append(changed, f.Filename)
		}
	}
	if len(changed) > 0 {
		fmt.Fprintf(out, "Build REJECTED: /gcbrun shared-db with changes under %s against %s; a migration on the shared database would change %s before any release:\n  %s\n", subs[migrationsSub], subs[defaultBranchSub], subs[envSub], strings.Join(changed, "\n  "))

		return errors.Newf("%s/gcbrun shared-db with changes under %s", rejected, subs[migrationsSub])
	}
	fmt.Fprintf(out, "shared-db: nothing under %s changed against %s.\n", subs[migrationsSub], subs[defaultBranchSub])

	return nil
}

// planChange is one resource change of a plan as tofu show -json writes it.
type planChange struct {
	Address string `json:"address"`
	Type    string `json:"type"`
	Change  struct {
		Actions []string       `json:"actions"`
		Before  map[string]any `json:"before"`
		After   map[string]any `json:"after"`
	} `json:"change"`
}

// identifying are the attributes that name a resource, where the pull request's name
// must appear. A Firestore ruleset is named by the service, so its source is read: the
// stack names the ruleset's file for the database the rules are released to. A bucket's
// policy is named by its bucket, the pull request's own.
var identifying = []string{keyName, "account_id", "service", "job", "database", "parent", "secret_id", "service_account_id", "bucket", "source"}

// planDocument is a plan as tofu show -json gives it: its resource changes and its
// output changes.
type planDocument struct {
	ResourceChanges []planChange                `json:"resource_changes"`
	OutputChanges   map[string]planOutputChange `json:"output_changes"`
}

// planOutputChange is an output's planned value; nil when it is not known yet.
type planOutputChange struct {
	After any `json:"after"`
}

// The actions a planned change carries.
const (
	actionCreate = "create"
	actionUpdate = "update"
	actionDelete = "delete"
)

// planOffenders reads the plan's changes and answers the ones that are not the pull
// request's ("address (actions)"), and how many changes the plan makes in all.
func planOffenders(data []byte, name string) (offenders []string, count int, err error) {
	var plan planDocument
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, 0, errors.Wrapf(err, "json.Unmarshal(): %s", PlanJSONFile)
	}
	for _, c := range plan.ResourceChanges {
		actions := slices.DeleteFunc(slices.Clone(c.Change.Actions), func(a string) bool {
			return a == "no-op" || a == "read"
		})
		if len(actions) == 0 {
			continue
		}
		count++
		if !strings.HasPrefix(c.Type, "google_") || ownedBy(c, name) {
			continue
		}
		offenders = append(offenders, fmt.Sprintf("%s (%s)", c.Address, strings.Join(c.Change.Actions, ",")))
	}

	return offenders, count, nil
}

// ownedBy reports whether the change is the pull request's: its naming attributes carry
// the name, or it is an IAM membership of one of the pull request's accounts.
func ownedBy(c planChange, name string) bool {
	values := c.Change.After
	if values == nil {
		values = c.Change.Before
	}
	var ident []string
	for _, attr := range identifying {
		if v, ok := values[attr]; ok && v != nil {
			ident = append(ident, jsonText(v))
		}
	}
	if strings.Contains(strings.Join(ident, " "), name) {
		return true
	}
	member, _ := values["member"].(string)

	return strings.HasSuffix(c.Type, "_iam_member") && strings.HasPrefix(member, "serviceAccount:"+name+"-")
}

// jsonText is a plan value as text: a string as it is, anything else as its JSON.
func jsonText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}

	return string(data)
}
