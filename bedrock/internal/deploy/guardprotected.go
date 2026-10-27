// guardprotected.go is the guard on an environment's plan against the destroy of a
// resource whose state records its protection: the secret containers, the file stores'
// buckets, the Spanner database and the Firestore database, which hold data and are
// protected in every environment. Google's provider refuses such a destroy at the apply
// alone, where a release has already merged and its run has started; the guard reads the
// plan's prior state and refuses it at the plan, in the pull request's check build and
// in the release's own plan step, so that a release whose code stops declaring a secret
// or a file store is refused until the release before it retired the variable (retired
// in terraform.tfvars), whose apply lifted the protection. A restore run and bedrock
// destroy lift the protection with an apply of their own before their plan, so their
// plans pass the guard.

package deploy

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
)

// protection is how a protected resource type records in the state that a destroy is
// held off, and what to say when a plan destroys one.
type protection struct {
	// attribute is the state attribute, and held reports its value holding the destroy
	// off.
	attribute string
	held      func(v any) bool
	// variable is the variable the resource is declared by, which a release retires to
	// lift the protection; "" for a resource no release retires (the database, a bucket
	// that is not a file store), which bedrock destroy alone takes.
	variable func(c *planChange) string
	// holds says what the resource holds, for the refusal.
	holds string
}

// protections are the protected types, by the resource type the plan names.
var protections = map[string]protection{
	"google_secret_manager_secret": {attribute: "deletion_protection", held: isTrue, variable: indexVariable, holds: "a secret container holds every version of its value"},
	"google_storage_bucket":        {attribute: "force_destroy", held: isFalse, variable: fileStoreVariable, holds: "a file store's bucket holds its objects"},
	"google_spanner_database":      {attribute: "deletion_protection", held: isTrue, holds: "the database holds the environment's rows"},
	"google_firestore_database":    {attribute: "delete_protection_state", held: isEnabled, variable: firestoreVariable, holds: "the Firestore database holds its documents"},
}

func isTrue(v any) bool {
	b, ok := v.(bool)

	return ok && b
}

func isFalse(v any) bool {
	b, ok := v.(bool)

	return ok && !b
}

func isEnabled(v any) bool {
	s, ok := v.(string)

	return ok && s == "DELETE_PROTECTION_ENABLED"
}

// indexVariable is the variable a secret container is declared by: the resource's index
// (google_secret_manager_secret.<app>["APP_X"]; a build secret's name for .build).
func indexVariable(c *planChange) string {
	s, _ := c.Index.(string)

	return s
}

// fileStoreVariable is the variable a file store's bucket is declared by, from the
// resource's name as storage.tf names it: files for APP_FILE_STORE, files_<name> for
// APP_FILE_STORE_<NAME>; "" for a bucket that is not a file store's.
func fileStoreVariable(c *planChange) string {
	name := strings.TrimPrefix(resourceName(c.Address), c.Type+".")
	switch {
	case name == fileStoreResource:
		return fileStoreVariablePrefix
	case strings.HasPrefix(name, fileStoreResource+"_"):
		return fileStoreVariablePrefix + "_" + strings.ToUpper(strings.TrimPrefix(name, fileStoreResource+"_"))
	default:
		return ""
	}
}

func firestoreVariable(*planChange) string {
	return firestoreDatabaseVariable
}

// The file stores' resource name and variable, as the stack and the code name them,
// and the Firestore database's variable.
const (
	fileStoreResource         = "files"
	fileStoreVariablePrefix   = "APP_FILE_STORE"
	firestoreDatabaseVariable = "APP_FIRESTORE_DATABASE"
)

// protectedDeletions reads the plan's JSON for the destroys of resources whose prior
// state records their protection, and answers one line per resource: what the plan
// destroys, what the state records, and the way on (retiring the variable a release
// before, or the command that lifts the protection itself). A replace counts as a
// destroy, since the resource it makes again holds nothing of the old one.
func protectedDeletions(data []byte) ([]string, error) {
	var plan planDocument
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal(): the plan's JSON")
	}
	var lines []string
	for i := range plan.ResourceChanges {
		c := &plan.ResourceChanges[i]
		p, ok := protections[c.Type]
		if !ok || !slices.Contains(c.Change.Actions, actionDelete) || !p.held(c.Change.Before[p.attribute]) {
			continue
		}
		lines = append(lines, p.line(c))
	}

	return lines, nil
}

// line is the refusal's line for one destroy of a protected resource.
func (p protection) line(c *planChange) string {
	recorded := fmt.Sprintf("%s = %v", p.attribute, c.Change.Before[p.attribute])
	var way string
	switch variable := p.variable; {
	case variable == nil:
		way = "bedrock destroy alone destroys it, and a restore run alone replaces it, each lifting the protection with an apply of its own first"
	case variable(c) == "":
		way = "bedrock destroy alone destroys it, lifting the protection with an apply of its own first"
	default:
		v := variable(c)
		way = fmt.Sprintf("a release that stops declaring it is refused until the release before it retired it: keep the declaration, name %s in retired in terraform.tfvars (retired = [%q]) and release, which lifts the protection; the next release removes the declaration and the name together", v, v)
	}

	return fmt.Sprintf("%s: the plan destroys it, and the state records its protection (%s); %s, so %s", c.Address, recorded, p.holds, way)
}

// guardProtected refuses a release's plan that destroys a protected resource, naming
// each (the tag build's plan step; the check build refuses the same through
// planEnvironment, on the pull request).
func guardProtected(data []byte, env string) error {
	held, err := protectedDeletions(data)
	if err != nil {
		return err
	}
	if len(held) == 0 {
		return nil
	}

	return errors.Newf("%sthe plan of %s's stack destroys what the state protects, and nothing was applied:\n  %s", rejected, env, strings.Join(held, "\n  "))
}
