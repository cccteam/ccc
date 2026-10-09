package firestore

import (
	"context"
	"slices"

	"github.com/go-playground/errors/v5"
)

// Settings are the variables the driver reads, declared on the application's
// configuration by embedding, so the stack and the development environment render them
// from the declaration: the Firestore database the live service runs on, beside the
// application's own database, where the subscriptions the generated handlers register,
// the change sets the browser listens to and the signals document the application's
// instances tell each other through live. The declaration the tools read is
// declaration.Settings, in the package beside this one, which holds nothing of the
// driver.
type Settings struct {
	// ProjectID is the Google Cloud project the database belongs to. A deployment that
	// names the database names its project too, or Open refuses: the database is not
	// assumed to be in the application's Spanner project, which, where environments
	// share a Spanner instance, is the shared instance's and not the environment's.
	// Against the emulator it may stay empty, and the project Open is handed stands in,
	// since the emulator takes any project id.
	ProjectID string `env:"GOOGLE_CLOUD_FIRESTORE_PROJECT"`
	// DatabaseID is the Firestore database, by id: how a deployment hands the database
	// to the application.
	DatabaseID string `env:"APP_FIRESTORE_DATABASE"`
	// APIKey is the Firebase web API key the browser initializes the SDK with; unused
	// against the emulator.
	APIKey string `env:"APP_FIREBASE_API_KEY"`
	// EmulatorHost is the Firestore emulator's host:port, the development stack's. Set,
	// the service talks to the emulator with its owner credential, answers the token
	// route with the host and no token, and revokes nothing.
	EmulatorHost string `env:"FIRESTORE_EMULATOR_HOST"`
}

// firebaseOrigins are the hosts the Firebase JS SDK reaches in production: Firestore's
// endpoint, which the change feed listens through, and Firebase Auth's two, which the
// custom token is signed in through and refreshed at.
var firebaseOrigins = []string{
	"https://firestore.googleapis.com",
	"https://identitytoolkit.googleapis.com",
	"https://securetoken.googleapis.com",
}

// BrowserOrigins returns the origins the browser connects to for the change feed, which
// the content security policy's connect-src must name beside the application itself:
// the emulator over plain HTTP in development (the token route hands the browser the
// same host), Firebase's hosts in production, and none while neither is configured.
func (s Settings) BrowserOrigins() []string {
	switch {
	case s.EmulatorHost != "":
		return []string{"http://" + s.EmulatorHost}
	case s.DatabaseID != "":
		return slices.Clone(firebaseOrigins)
	default:
		return nil
	}
}

// project is the project the service opens the database in: ProjectID, or, against the
// emulator with ProjectID empty, emulatorProject, which the emulator takes as it takes
// any project id. A database named without its project is refused, naming both
// variables, as is a configuration naming neither a database nor the emulator, since
// the live service is required.
func (s Settings) project(emulatorProject string) (string, error) {
	switch {
	case s.DatabaseID == "" && s.EmulatorHost == "":
		return "", errors.New("the live service needs a Firestore database: set APP_FIRESTORE_DATABASE (the database id, a deployment) or FIRESTORE_EMULATOR_HOST (the emulator, development)")
	case s.ProjectID != "":
		return s.ProjectID, nil
	case s.EmulatorHost != "":
		return emulatorProject, nil
	default:
		return "", errors.New("APP_FIRESTORE_DATABASE names a Firestore database and GOOGLE_CLOUD_FIRESTORE_PROJECT names no project for it: set GOOGLE_CLOUD_FIRESTORE_PROJECT to the project the database is in, which is not assumed to be the Spanner project")
	}
}

// Open opens the live service on the database the settings name, or on the emulator:
// the driver's entry, over New. emulatorProject is the project the emulator's database
// is opened under when the settings name none, the application's Spanner project in the
// skeleton, since the emulator takes any project id and the browser connects under the
// one the token route names. A database named without its project is refused, naming
// both variables, as is a configuration naming neither a database nor the emulator,
// since the live service is required.
func Open(ctx context.Context, s Settings, emulatorProject string, opts ...Option) (*Service, error) {
	project, err := s.project(emulatorProject)
	if err != nil {
		return nil, err
	}

	return New(ctx, Config{ProjectID: project, DatabaseID: s.DatabaseID, APIKey: s.APIKey, EmulatorHost: s.EmulatorHost}, opts...)
}
