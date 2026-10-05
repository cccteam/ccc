// buildargs.go holds the build arguments a placement declares: values the stack makes in
// each environment, which the image build takes under the name the application's
// Dockerfile declares with ARG. A value such as the Firebase web API key exists only once
// the stack is applied, so no person can write it per environment the way a declared
// substitution in terraform.tfvars is written.

package derive

import (
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
)

// The values a placement's buildArguments may name: the catalog of what the stack makes
// or knows in each environment.
const (
	// BuildValueFirebaseAPIKey is the key string of the Firebase web API key the stack
	// makes for the application (firestore.tf), the key the browser presents to sign in.
	BuildValueFirebaseAPIKey = "firebaseApiKey"
	// BuildValueFirestoreDatabase is the id of the application's Firestore database.
	BuildValueFirestoreDatabase = "firestoreDatabase"
	// BuildValueProjectID is the environment project's id.
	BuildValueProjectID = "projectId"
	// BuildValueEnvironment is the environment's name (tst, stg, prd).
	BuildValueEnvironment = "environment"
	// BuildValueHostname is the service's canonical hostname in the environment.
	BuildValueHostname = "hostname"
)

// BuildValue is one entry of the catalog: its name, as buildArguments writes it, and what
// the value is.
type BuildValue struct {
	Name string
	What string
}

// BuildValues is the catalog, in the order the messages and the READMEs list it.
func BuildValues() []BuildValue {
	return []BuildValue{
		{Name: BuildValueFirebaseAPIKey, What: "the key string of the Firebase web API key the stack makes for the application, which the browser presents to sign in (made when the code declares " + varFirebaseAPIKey + ")"},
		{Name: BuildValueFirestoreDatabase, What: "the id of the application's Firestore database (made when the code declares " + varFirestoreDatabase + ")"},
		{Name: BuildValueProjectID, What: "the environment project's id"},
		{Name: BuildValueEnvironment, What: "the environment's name"},
		{Name: BuildValueHostname, What: "the service's canonical hostname in the environment"},
	}
}

// BuildValueNames lists the catalog's names, in its order.
func BuildValueNames() []string {
	values := BuildValues()
	names := make([]string, 0, len(values))
	for _, v := range values {
		names = append(names, v.Name)
	}

	return names
}

// BuildArgumentPrefix starts the trigger substitution that carries a build argument,
// _BUILD_ARG_<NAME>: the stack renders one per declared argument, the image build reads
// every substitution so named and passes it as --build-arg NAME=value, and a substitution
// the application declares in terraform.tfvars may not start with it.
const BuildArgumentPrefix = "_BUILD_ARG_"

// BuildArgumentSubstitution is the trigger substitution carrying the build argument.
func BuildArgumentSubstitution(name string) string {
	return BuildArgumentPrefix + name
}

// pipelineBuildArguments are the build arguments the pipeline passes on its own: the
// release and the commit. A declared argument may not take one of their names.
var pipelineBuildArguments = []string{"VERSION", "COMMIT"}

// buildArgumentRE is a build argument's name: an uppercase identifier, as a Dockerfile's
// ARG declares it and a trigger substitution's key admits it.
var buildArgumentRE = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// validateBuildArguments checks each declared build argument: its name an uppercase
// identifier other than the pipeline's own, its value one of the catalog's.
func (p *Placement) validateBuildArguments() error {
	for _, name := range p.BuildArgumentNames() {
		value := p.BuildArguments[name]
		if !buildArgumentRE.MatchString(name) {
			return errors.Newf("buildArguments names %q: a build argument's name is an uppercase identifier (FIREBASE_API_KEY), the name the Dockerfile declares with ARG", name)
		}
		if slices.Contains(pipelineBuildArguments, name) {
			return errors.Newf("buildArguments names %s, a build argument the pipeline passes itself (%s): give the value another name", name, strings.Join(pipelineBuildArguments, ", "))
		}
		if !slices.Contains(BuildValueNames(), value) {
			return errors.Newf("buildArguments.%s is %q, which is not one of the values the stack makes: %s", name, value, strings.Join(BuildValueNames(), ", "))
		}
	}

	return nil
}

// BuildArgumentNames are the names of the declared build arguments, sorted.
func (p *Placement) BuildArgumentNames() []string {
	return slices.Sorted(maps.Keys(p.BuildArguments))
}

// buildArguments refuses a build argument naming a value the stack makes only for code
// that declares it: the Firebase web API key without APP_FIREBASE_API_KEY, the Firestore
// database without APP_FIRESTORE_DATABASE. The stack would have no resource to read the
// value from.
func (m *Model) buildArguments() error {
	for _, name := range m.Placement.BuildArgumentNames() {
		value := m.Placement.BuildArguments[name]
		switch {
		case value == BuildValueFirebaseAPIKey && m.byRole(RoleFirebaseAPIKey) == nil:
			return errors.Newf("buildArguments.%s names %s, the Firebase web API key the stack makes only when the config package declares %s, which it does not", name, value, varFirebaseAPIKey)
		case value == BuildValueFirestoreDatabase && m.byRole(RoleFirestoreDatabase) == nil:
			return errors.Newf("buildArguments.%s names %s, the Firestore database the stack makes only when the config package declares %s, which it does not", name, value, varFirestoreDatabase)
		}
	}

	return nil
}
