// spanner.go is the Spanner seam of a restore from production's backup: the environment's
// database is dropped and restored from the most recent backup of production's database,
// on the instance they share, as the apply identity.

package deploy

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/go-playground/errors/v5"
	"golang.org/x/oauth2"
	"google.golang.org/api/impersonate"
)

// spannerAPI is where databases are dropped and restored and backups are listed.
const spannerAPI = "https://spanner.googleapis.com"

// Backup is one backup of a database as the restore reads it.
type Backup struct {
	// Name is the backup's resource name (projects/<p>/instances/<i>/backups/<b>).
	Name string
	// VersionTime is the moment the backup's data is from; CreateTime when it was taken.
	VersionTime string
	CreateTime  string
}

// Spanner drops and restores databases and lists backups: the v1 API, or a fake in tests.
type Spanner interface {
	// LatestBackup is the most recently created READY backup of the database
	// (projects/<p>/instances/<i>/databases/<d>) on its instance; nil when it has none.
	LatestBackup(ctx context.Context, instance, database string) (*Backup, error)
	// DropDatabase drops the database (projects/<p>/instances/<i>/databases/<d>); a
	// database that is already gone is no error.
	DropDatabase(ctx context.Context, database string) error
	// RestoreDatabase creates the database id on the instance from the backup and waits
	// for the restore to end.
	RestoreDatabase(ctx context.Context, instance, databaseID, backup string) error
}

// SpannerAsFunc opens Spanner as an impersonated identity: the apply identity, which holds
// database admin on the environment's instance.
type SpannerAsFunc func(ctx context.Context, identity string) (Spanner, error)

// NewSpannerAs opens the Spanner v1 REST API as the identity.
func NewSpannerAs(ctx context.Context, identity string) (Spanner, error) {
	source, err := impersonate.CredentialsTokenSource(ctx, impersonate.CredentialsConfig{
		TargetPrincipal: identity,
		Scopes:          []string{cloudPlatformScope},
	})
	if err != nil {
		return nil, errors.Wrapf(err, "impersonate.CredentialsTokenSource(): %s", identity)
	}

	return &spanner{cloudRun: &cloudRun{http: oauth2.NewClient(ctx, source), base: spannerAPI, poll: 10 * time.Second, service: "Spanner"}}, nil
}

// spanner is Spanner over the v1 API; it reuses the Cloud Run client's calling.
type spanner struct {
	*cloudRun
}

func (s *spanner) LatestBackup(ctx context.Context, instance, database string) (*Backup, error) {
	query := url.Values{}
	query.Set("filter", `database:"`+database+`" AND state:READY`)
	backups, err := s.list(ctx, "/v1/"+instance+"/backups?"+query.Encode(), "backups")
	if err != nil {
		return nil, err
	}
	var found []*Backup
	for _, b := range backups {
		// The filter is a match on the backup's fields; the database is checked exactly.
		if text(b, "database") != database || text(b, "state") != "READY" {
			continue
		}
		found = append(found, &Backup{Name: text(b, keyName), VersionTime: text(b, "versionTime"), CreateTime: text(b, "createTime")})
	}
	if len(found) == 0 {
		return nil, nil
	}
	sort.Slice(found, func(i, j int) bool {
		return found[i].CreateTime > found[j].CreateTime
	})

	return found[0], nil
}

func (s *spanner) DropDatabase(ctx context.Context, database string) error {
	if _, err := s.call(ctx, http.MethodDelete, "/v1/"+database, nil); err != nil && !isNotFound(err) {
		return err
	}

	return nil
}

func (s *spanner) RestoreDatabase(ctx context.Context, instance, databaseID, backup string) error {
	op, err := s.call(ctx, http.MethodPost, "/v1/"+instance+"/databases:restore", map[string]any{"databaseId": databaseID, "backup": backup})
	if err != nil {
		return err
	}
	name, _ := op[keyName].(string)
	if name == "" {
		return errors.New("Spanner answered no operation name")
	}
	for {
		if done, _ := op["done"].(bool); done {
			if apiErr, ok := op["error"].(map[string]any); ok {
				msg, _ := apiErr["message"].(string)

				return errors.Newf("Spanner operation %s failed: %s", name, msg)
			}

			return nil
		}
		select {
		case <-ctx.Done():
			return errors.Wrapf(ctx.Err(), "waiting for Spanner operation %s", name)
		case <-time.After(s.poll):
		}
		if op, err = s.call(ctx, http.MethodGet, "/v1/"+name, nil); err != nil {
			return err
		}
	}
}
