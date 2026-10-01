// firestore.go is the Firestore seam: a restore run deletes every document of the
// environment's Firestore database, whose documents refer to rows the restore replaces.
// The database itself stays: Firestore keeps a deleted database's id unavailable for
// minutes, so a database cannot be replaced within one apply.

package deploy

import (
	"context"
	"net/http"
	"time"

	"github.com/go-playground/errors/v5"
	"golang.org/x/oauth2"
	"google.golang.org/api/impersonate"
)

// firestoreAPI is where a database's documents are deleted.
const firestoreAPI = "https://firestore.googleapis.com"

// Firestore deletes a database's documents: the v1 API, or a fake in tests.
type Firestore interface {
	// DeleteAllDocuments deletes every document of the database (projects/<p>/databases/<d>),
	// in every collection, and waits for the deletion to end.
	DeleteAllDocuments(ctx context.Context, database string) error
}

// FirestoreAsFunc opens Firestore as an impersonated identity: the apply identity, which
// owns the environment's data stores, since the deploy identity holds no data role.
type FirestoreAsFunc func(ctx context.Context, identity string) (Firestore, error)

// NewFirestoreAs opens the Firestore v1 REST API as the identity.
func NewFirestoreAs(ctx context.Context, identity string) (Firestore, error) {
	source, err := impersonate.CredentialsTokenSource(ctx, impersonate.CredentialsConfig{
		TargetPrincipal: identity,
		Scopes:          []string{cloudPlatformScope},
	})
	if err != nil {
		return nil, errors.Wrapf(err, "impersonate.CredentialsTokenSource(): %s", identity)
	}

	return &firestore{cloudRun: &cloudRun{http: oauth2.NewClient(ctx, source), base: firestoreAPI, poll: 5 * time.Second}}, nil
}

// firestore is Firestore over the v1 API; it reuses the Cloud Run client's calling.
type firestore struct {
	*cloudRun
}

func (f *firestore) DeleteAllDocuments(ctx context.Context, database string) error {
	op, err := f.call(ctx, http.MethodPost, "/v1/"+database+":bulkDeleteDocuments", map[string]any{})
	if err != nil {
		return err
	}
	name, _ := op[keyName].(string)
	if name == "" {
		return errors.New("Firestore answered no operation name")
	}
	for {
		if done, _ := op["done"].(bool); done {
			if apiErr, ok := op["error"].(map[string]any); ok {
				msg, _ := apiErr["message"].(string)

				return errors.Newf("Firestore operation %s failed: %s", name, msg)
			}

			return nil
		}
		select {
		case <-ctx.Done():
			return errors.Wrapf(ctx.Err(), "waiting for Firestore operation %s", name)
		case <-time.After(f.poll):
		}
		if op, err = f.call(ctx, http.MethodGet, "/v1/"+name, nil); err != nil {
			return err
		}
	}
}
