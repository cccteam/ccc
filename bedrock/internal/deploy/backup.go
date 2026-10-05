// backup.go is the release backup: in an environment the placement's releaseBackups names
// (production alone unless it says otherwise), a release build starts, before its
// migrations, a Spanner backup of the environment's database as of that moment, the cut,
// kept fourteen days, as the apply identity, and goes on without waiting for it. The
// backup is what bedrock rollback restores when the release goes wrong there; the record
// names it. A build that cannot start the backup stops here, before any migration ran.

package deploy

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
)

// The facts the release backup step leaves: the cut, the backup's resource name, the
// moment its data is from and when Spanner deletes it; and the generation of the database
// the stack plan was told (1 unless a rollback moved it), which the stack steps leave.
const (
	cutFact                  = "CUT"
	releaseBackupFact        = "RELEASE_BACKUP"
	releaseBackupTimeFact    = "RELEASE_BACKUP_TIME"
	releaseBackupExpiresFact = "RELEASE_BACKUP_EXPIRES"
	databaseGenerationFact   = "DATABASE_GENERATION"
	// releaseBackupKeep is how long a release backup is kept: a release that goes wrong
	// is rolled back within days, and the weekly and daily schedules keep the longer
	// history.
	releaseBackupKeep = 14 * 24 * time.Hour
	// releaseBackupInfix joins the database's name and the release in the backup's id.
	releaseBackupInfix = "-pre-"
)

// StartReleaseBackup takes the release backup when the run is a tag build in an
// environment on the placement's releaseBackups list that applies its migrations: the
// cut is now, the backup is named after the database, the release and the build
// (<database>-pre-<release with dashes>-<the build id's first eight characters>, so a
// release run again keeps a backup of its own cut), its data is as of the cut and it is
// kept fourteen days; the facts carry it to the record. A pull-request build, a run that
// deploys nothing, a run that applies no migration, a restore run (which replaces the
// database) and a rollback run (which restores a backup) take none, each said on out.
func StartReleaseBackup(ctx context.Context, clients *Clients, w Workspace, now time.Time, out io.Writer) error {
	subs, ok, err := tagBuildStep(w, out)
	if err != nil || !ok {
		return err
	}
	env, err := w.Environment()
	if err != nil {
		return err
	}
	if reason := noReleaseBackup(env); reason != "" {
		fmt.Fprintf(out, "No release backup: %s\n", reason)

		return nil
	}
	database, instance, err := spannerDatabase(env)
	if err != nil {
		return err
	}
	release := env[releaseFact]
	if release == "" {
		return errors.Newf("%s exports no %s: the resolve step did not run", EnvironmentFile, releaseFact)
	}
	identity := subs[applyIdentitySub]
	if identity == "" {
		return errors.Newf("%s names no apply identity (%s): the stack's triggers carry it", BuildFile, applyIdentitySub)
	}
	b, err := w.Build()
	if err != nil {
		return err
	}
	cut := now.UTC().Truncate(time.Second)
	expires := cut.Add(releaseBackupKeep)
	id := path.Base(database) + releaseBackupInfix + strings.ReplaceAll(release, ".", "-") + "-" + shortBuildID(b.ID)
	store, err := clients.SpannerAs(ctx, identity)
	if err != nil {
		return err
	}
	operation, err := store.CreateBackup(ctx, instance, id, database, cut, expires)
	if err != nil {
		return errors.Wrapf(err, "starting the release backup %s of %s as of %s", id, path.Base(database), cut.Format(time.RFC3339))
	}
	name := instance + "/backups/" + id
	fmt.Fprintf(out, "=== Release backup: %s holds %s as of the cut, %s, and is kept until %s (operation %s) ===\n", id, path.Base(database), cut.Format(time.RFC3339), expires.Format(time.RFC3339), operation)
	fmt.Fprintf(out, "Spanner takes the backup in the background while %s goes on; the migrations that follow change the live database alone. A release gone wrong is rolled back to it with bedrock rollback %s.\n", release, subs[envSub])

	return w.Append(map[string]string{cutFact: cut.Format(time.RFC3339), releaseBackupFact: name, releaseBackupTimeFact: cut.Format(time.RFC3339), releaseBackupExpiresFact: expires.Format(time.RFC3339)})
}

// shortBuildID is the build id's first eight characters (a UUID's first group), enough
// to tell two builds of one release apart in a backup's name.
func shortBuildID(id string) string {
	if len(id) > 8 {
		id = id[:8]
	}

	return strings.TrimRight(id, "-")
}

// noReleaseBackup says why the run takes no release backup, or nothing when it does.
func noReleaseBackup(env map[string]string) string {
	switch {
	case env[skipDeploy] == trueValue:
		return "nothing deploys in this run (" + env[skipReasonFact] + ")."
	case env[keepsReleaseBackupsFact] != trueValue:
		return "the environment is not on the placement's releaseBackups list, so no backup is taken as of the cut and bedrock rollback does not serve it."
	case env[rollbackFact] != "":
		return "a rollback restores a backup into the database's next generation; the state before it is that backup, and the live database stays as the forensic copy."
	case env[restoreFact] != "":
		return "a restore run replaces the database (" + env[restoreFact] + "), so there is no state before the migrations to keep."
	case env[runMigrationsFact] != trueValue:
		return "this run applies no migration, so the database stays as the live release left it."
	}

	return ""
}

// spannerDatabase is the Spanner database the stack steps named for the migrate command
// (the current generation), with its instance.
func spannerDatabase(env map[string]string) (database, instance string, err error) {
	databases, err := migrateDatabases(env)
	if err != nil {
		return "", "", err
	}
	for _, d := range databases {
		if i := strings.Index(d, "/databases/"); i > 0 && strings.Contains(d, "/instances/") {
			return d, d[:i], nil
		}
	}

	return "", "", errors.Newf("%s names no Spanner database (%s): the stack steps write the migrate databases from the stack's substitutions output", EnvironmentFile, migrateDatabasesFact)
}
