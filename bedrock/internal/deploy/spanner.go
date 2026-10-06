// spanner.go is the Spanner seam of the restores and the rollback: a restore from
// production's backup drops the environment's database and restores it from the most
// recent backup of production's database, on the instance they share; a release build
// takes a backup of the database as of its cut; a rollback takes a forensic backup of the
// live database and restores a backup into the database's next generation. All as the
// apply identity.

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

// spannerAPI is where databases are dropped and restored and backups are listed;
// keyDatabase is the field naming a backup's database.
const (
	spannerAPI  = "https://spanner.googleapis.com"
	keyDatabase = "database"
)

// Backup is one backup of a database as the restore reads it.
type Backup struct {
	// Name is the backup's resource name (projects/<p>/instances/<i>/backups/<b>).
	Name string
	// VersionTime is the moment the backup's data is from; CreateTime when it was taken.
	VersionTime string
	CreateTime  string
	// Database is the database the backup was taken from (projects/<p>/instances/<i>/databases/<d>);
	// State is CREATING while Spanner takes it and READY once it can be restored.
	Database string
	State    string
	// ExpireTime is when Spanner deletes the backup.
	ExpireTime string
}

// Spanner's backup states.
const (
	BackupReady    = "READY"
	BackupCreating = "CREATING"
)

// Database is a Spanner database as its state matters here: READY (or READY_OPTIMIZING,
// serving while Spanner finishes a restore's optimization) serves, and CREATING is a
// restore still in progress, from the backup RestoredFrom names.
type Database struct {
	Name         string
	State        string
	RestoredFrom string
}

// Spanner's database states that serve.
const (
	DatabaseReady           = "READY"
	DatabaseReadyOptimizing = "READY_OPTIMIZING"
)

// Spanner drops and restores databases and lists backups: the v1 API, or a fake in tests.
type Spanner interface {
	// LatestBackup is the most recently created backup of the database
	// (projects/<p>/instances/<i>/databases/<d>) on its instance, READY or still CREATING
	// (a backup Spanner is taking, a release's started minutes ago, holds the newest data,
	// and a restore waits for it); nil when it has none.
	LatestBackup(ctx context.Context, instance, database string) (*Backup, error)
	// DropDatabase drops the database (projects/<p>/instances/<i>/databases/<d>); a
	// database that is already gone is no error.
	DropDatabase(ctx context.Context, database string) error
	// RestoreDatabase creates the database id on the instance from the backup and waits
	// for the restore to end.
	RestoreDatabase(ctx context.Context, instance, databaseID, backup string) error
	// CreateBackup starts a backup of the database (projects/<p>/instances/<i>/databases/<d>)
	// as of versionTime, named backupID on the instance and kept until expireTime, and
	// answers the operation's name without waiting for it.
	CreateBackup(ctx context.Context, instance, backupID, database string, versionTime, expireTime time.Time) (string, error)
	// Backup reads one backup by resource name; nil when there is none.
	Backup(ctx context.Context, name string) (*Backup, error)
	// Database reads one database by resource name (projects/<p>/instances/<i>/databases/<d>):
	// its state and, for one a restore made, the backup it was restored from; nil when
	// there is none.
	Database(ctx context.Context, name string) (*Database, error)
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

	return &spanner{cloudRun: &cloudRun{http: oauth2.NewClient(ctx, source), base: spannerAPI, poll: 10 * time.Second, service: serviceSpanner}}, nil
}

// spanner is Spanner over the v1 API; it reuses the Cloud Run client's calling.
type spanner struct {
	*cloudRun
}

func (s *spanner) LatestBackup(ctx context.Context, instance, database string) (*Backup, error) {
	query := url.Values{}
	query.Set("filter", `database:"`+database+`"`)
	backups, err := s.list(ctx, "/v1/"+instance+"/backups?"+query.Encode(), "backups")
	if err != nil {
		return nil, err
	}
	var found []*Backup
	for _, b := range backups {
		// The filter is a match on the backup's fields; the database is checked exactly.
		state := text(b, "state")
		if text(b, keyDatabase) != database || (state != BackupReady && state != BackupCreating) {
			continue
		}
		found = append(found, &Backup{Name: text(b, keyName), VersionTime: text(b, "versionTime"), CreateTime: text(b, "createTime"), Database: database, State: state})
	}
	if len(found) == 0 {
		return nil, nil
	}
	sort.Slice(found, func(i, j int) bool {
		return found[i].CreateTime > found[j].CreateTime
	})

	return found[0], nil
}

func (s *spanner) CreateBackup(ctx context.Context, instance, backupID, database string, versionTime, expireTime time.Time) (string, error) {
	query := url.Values{}
	query.Set("backupId", backupID)
	body := map[string]any{keyDatabase: database, "versionTime": versionTime.UTC().Format(time.RFC3339Nano), "expireTime": expireTime.UTC().Format(time.RFC3339Nano)}
	op, err := s.call(ctx, http.MethodPost, "/v1/"+instance+"/backups?"+query.Encode(), body)
	if err != nil {
		return "", err
	}
	name, _ := op[keyName].(string)
	if name == "" {
		return "", errors.New("Spanner answered no operation name")
	}
	if apiErr, ok := op["error"].(map[string]any); ok {
		msg, _ := apiErr["message"].(string)

		return "", errors.Newf("Spanner refused the backup %s: %s", backupID, msg)
	}

	return name, nil
}

func (s *spanner) Backup(ctx context.Context, name string) (*Backup, error) {
	b, err := s.call(ctx, http.MethodGet, "/v1/"+name, nil)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}

		return nil, err
	}

	return &Backup{Name: text(b, keyName), VersionTime: text(b, "versionTime"), CreateTime: text(b, "createTime"), Database: text(b, keyDatabase), State: text(b, "state"), ExpireTime: text(b, "expireTime")}, nil
}

func (s *spanner) Database(ctx context.Context, name string) (*Database, error) {
	d, err := s.call(ctx, http.MethodGet, "/v1/"+name, nil)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}

		return nil, err
	}
	restored := ""
	if info, ok := d["restoreInfo"].(map[string]any); ok {
		if backup, ok := info["backupInfo"].(map[string]any); ok {
			restored = text(backup, "backup")
		}
	}

	return &Database{Name: text(d, keyName), State: text(d, "state"), RestoredFrom: restored}, nil
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
