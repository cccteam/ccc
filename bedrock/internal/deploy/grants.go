// grants.go is the wait, before the migrate command runs, for the deploy identity's grants
// on the application's databases: the stack's apply in the same build may have just made
// them (an application's first release, or the first after its grants changed), and IAM
// makes a new grant effective seconds to minutes after the policy holds it, so a command
// started at once is refused on a grant the apply has already written. The stack names
// the databases the command reaches (_MIGRATE_DATABASES, read back from the applied stack
// like the command's settings), and the step reads each one as the worker until it may.

package deploy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
	"golang.org/x/oauth2/google"
)

// The wait for a grant to take effect, bounded well above the minute IAM usually needs
// and far below the step's timeout; a read not yet allowed is tried again after the poll.
const (
	grantsWait = 3 * time.Minute
	grantsPoll = 5 * time.Second
)

// Grants reads the application's databases as the worker, the deploy identity: the real
// APIs, or a fake in tests.
type Grants interface {
	// Read reads the database named by its resource name (a Spanner database,
	// projects/<p>/instances/<i>/databases/<d>, or a Firestore one,
	// projects/<p>/databases/<d>): nil when the identity may, the API's error when it
	// may not yet (403) or when the read failed otherwise.
	Read(ctx context.Context, database string) error
}

// GrantsFunc opens Grants.
type GrantsFunc func(ctx context.Context) (Grants, error)

// NewGrants opens the Spanner and Firestore v1 REST APIs with the process's default
// credentials, the worker's identity.
func NewGrants(ctx context.Context) (Grants, error) {
	client, err := google.DefaultClient(ctx, cloudPlatformScope)
	if err != nil {
		return nil, errors.Wrap(err, "google.DefaultClient()")
	}

	return &grants{
		spanner:   &cloudRun{http: client, base: spannerAPI, service: serviceSpanner},
		firestore: &cloudRun{http: client, base: firestoreAPI, service: serviceFirestore},
	}, nil
}

// grants reads through the shared REST caller.
type grants struct {
	spanner, firestore *cloudRun
}

// Read gets a Spanner database's description, or one document of a Firestore database,
// which exists or not (a 404 is a read the identity was allowed to make).
func (g *grants) Read(ctx context.Context, database string) error {
	if strings.Contains(database, "/instances/") {
		_, err := g.spanner.call(ctx, http.MethodGet, "/v1/"+database, nil)

		return err
	}
	_, err := g.firestore.call(ctx, http.MethodGet, "/v1/"+database+"/documents/bedrock/grants", nil)
	if isNotFound(err) {
		return nil
	}

	return err
}

// notYet reports an error worth another try: the API answering 403 (the grant is not
// in effect) or failing on its side (5xx).
func notYet(err error) bool {
	var e *apiError

	return errors.As(err, &e) && (e.status == http.StatusForbidden || e.status >= http.StatusInternalServerError)
}

// awaitGrants reads every database the stack names until the deploy identity may read
// each, or the wait runs out, and says what it found. Without a Grants client (tests of
// the command alone) nothing is read.
func awaitGrants(ctx context.Context, clients *Clients, databases []string, out io.Writer) error {
	if clients.Grants == nil || len(databases) == 0 {
		return nil
	}
	g, err := clients.Grants(ctx)
	if err != nil {
		return err
	}
	pending := slices.Clone(databases)
	var waited time.Duration
	for {
		var still []string
		var last error
		for _, database := range pending {
			err := g.Read(ctx, database)
			switch {
			case err == nil:
			case notYet(err):
				still = append(still, database)
				last = err
			default:
				return errors.Wrapf(err, "reading %s as the deploy identity", database)
			}
		}
		if len(still) == 0 {
			break
		}
		if waited >= grantsWait {
			return errors.Newf("the deploy identity's grant on %s is not in effect after %s: %s; the stack applied it in this build, and IAM usually makes a grant effective within a minute", strings.Join(still, ", "), waited, last.Error())
		}
		if waited == 0 {
			fmt.Fprintf(out, "Waiting for the deploy identity's grant on %s to take effect (the stack applied it; IAM makes a grant effective within minutes).\n", strings.Join(still, ", "))
		}
		if err := clients.sleep()(ctx, grantsPoll); err != nil {
			return err
		}
		waited += grantsPoll
		pending = still
	}
	if waited > 0 {
		fmt.Fprintf(out, "The grants are in effect after %s: the deploy identity reads %s.\n", waited, strings.Join(databases, ", "))

		return nil
	}
	fmt.Fprintf(out, "The deploy identity reads %s.\n", strings.Join(databases, ", "))

	return nil
}
