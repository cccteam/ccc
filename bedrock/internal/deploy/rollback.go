// rollback.go is the two runs that return an environment to an earlier state, one for
// the code and one for the database. bedrock rollback runs the environment's rollback
// trigger at the release the environment returns to with nothing of the database: the
// earlier release's build again, no migration run, no backup taken, the generation
// unchanged. bedrock restore with a backup's name, @<moment> or a release's cut runs the
// version trigger at the live release with the restore instruction, and the pipeline keeps
// the live database as the forensic copy (a backup of it is taken as of now), restores the
// chosen backup into the database's next generation, writes the generation beside the
// deployment records as the fact every later build reads, adopts the restored database
// into the stack and points the stack at it, so the release's migrations, its revision and
// its traffic land on the restored data. Nothing is dropped, and no database an earlier
// run left is ever put back into service: a moment in an earlier generation's history is
// taken as a backup of that generation and restored into a new one.

package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
)

// The facts a generation restore leaves beyond the instruction's: the forensic backup of
// the live database, the database restored into and the one kept, and the generation
// before this run's (the backup restored and the moment its data is from are the restore
// facts every restore leaves, RESTORE_BACKUP and RESTORE_BACKUP_TIME).
const (
	restoreForensicFact     = "RESTORE_FORENSIC"
	restoreIntoFact         = "RESTORE_INTO"
	restoreKeptFact         = "RESTORE_KEPT"
	previousGenerationFact  = "DATABASE_PREVIOUS_GENERATION"
	restoreMomentPrefix     = "@"
	forensicBackupInfix     = "-forensic-"
	pointInTimeBackupInfix  = "-pit-"
	forensicBackupKeep      = 30 * 24 * time.Hour
	pointInTimeBackupKeep   = releaseBackupKeep
	backupStamp             = "20060102-1504"
	generationPrefix        = "database"
	backupWaitPoll          = 30 * time.Second
	backupWaitSays          = 5 * time.Minute
	databaseBackupsSegment  = "/backups/"
	databaseDatabaseSegment = "/databases/"
)

// rollback reads the rollback instruction: the rollback trigger alone carries _ROLLBACK,
// the release the environment leaves (its live release when the rollback was asked for),
// with who asked (_REQUESTER) and why (_REASON); a pull-request build carries none, and a
// rollback goes with no restore and no migration operation. A rollback run applies no
// migration: the database stays as the release left it, and the earlier release runs on
// it.
func (f *Facts) rollback() error {
	from, reason, requester := f.Substitutions[rollbackSub], f.Substitutions[reasonSub], f.Substitutions[requesterSub]
	if from == "" {
		return nil
	}
	switch {
	case f.Tag == "":
		return errors.Newf("%s=%s on a pull-request build: a rollback is a release build's instruction, run by bedrock rollback on the environment's rollback trigger", rollbackSub, from)
	case f.Restore != "":
		return errors.Newf("%s=%s with %s=%s: a rollback returns the code and a restore the database; a run does one, and both means one run after the other", rollbackSub, from, restoreSub, f.Restore)
	case f.Substitutions[migrateActionSub] != "":
		return errors.Newf("%s=%s with %s=%s: a rollback runs no migration; there is no migration state to operate on", rollbackSub, from, migrateActionSub, f.Substitutions[migrateActionSub])
	case !releaseTagRE.MatchString(from):
		return errors.Newf("%s=%q is not a release tag (v<major>.<minor>.<patch>): the release the environment leaves", rollbackSub, from)
	case from == f.Tag:
		return errors.Newf("%s=%s is the release this build is of: a rollback returns to an earlier release", rollbackSub, from)
	case requester == "":
		return errors.Newf("%s=%s names no requester (%s): a rollback says who asked for it", rollbackSub, from, requesterSub)
	case reason == "":
		return errors.Newf("%s=%s gives no reason (%s): a rollback says why it was asked for", rollbackSub, from, reasonSub)
	}
	f.Rollback, f.RollbackReason, f.Requester = from, reason, requester
	f.RunMigrations = false

	return nil
}

// releaseTagRE is a release tag, v<major>.<minor>.<patch>.
var releaseTagRE = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// statement is what a rollback run prints first, in the resolve step: the whole of what
// it does, before anything happens.
func (f *Facts) statement(out io.Writer) {
	if f.Rollback == "" {
		return
	}
	approved := ""
	if f.Approver != "" {
		approved = ", approved by " + f.Approver
	}
	fmt.Fprintf(out, "=== ROLLBACK of %s: %s returns to %s from %s, asked for by %s%s: %s ===\n", f.Environment, f.Substitutions[appSub], f.Tag, f.Rollback, f.Requester, approved, f.RollbackReason)
	fmt.Fprintf(out, "Nothing of the database: it stays as %s left it, every migration it holds applied, and %s runs on it. No migration runs, no backup is taken, nothing is restored; %s's build runs again and deploys as a release does. The database is returned by bedrock restore, in a run of its own.\n", f.Rollback, f.Tag, f.Tag)
}

// Rollback is a rollback run as the record keeps it: who asked, why, and the release the
// environment left. The record's migrations list what the database holds, by the live
// record the rollback found, since the run applied none.
type Rollback struct {
	Requester string `json:"requester"`
	Reason    string `json:"reason"`
	// From is the release the environment left, live when the rollback was asked for.
	From string `json:"from"`
}

// rollbackOf reads a rollback run's note from its facts; nil for any other run.
func rollbackOf(env map[string]string) *Rollback {
	if env[rollbackFact] == "" {
		return nil
	}

	return &Rollback{Requester: env[requesterFact], Reason: env[rollbackReasonFact], From: env[rollbackFact]}
}

// generationObject is the object under which a rollback leaves the environment's database
// generation beside the deployment records: <app>/database/<env>/<n>.json, a prefix no
// record reader lists.
func generationObject(app, env string, generation int) string {
	return app + "/" + generationPrefix + "/" + env + "/" + strconv.Itoa(generation) + ".json"
}

// databaseGeneration is the generation of the environment's database the stack is told:
// the highest a rollback has written under <app>/database/<env>/, or 1 when none has.
func databaseGeneration(ctx context.Context, store Store, bucket, app, env string) (int, error) {
	objects, err := store.List(ctx, bucket, app+"/"+generationPrefix+"/"+env+"/")
	if err != nil {
		return 0, errors.Wrapf(err, "listing gs://%s/%s/%s/%s/", bucket, app, generationPrefix, env)
	}
	generation := 1
	for _, object := range objects {
		n, err := strconv.Atoi(strings.TrimSuffix(path.Base(object), ".json"))
		if err == nil && n > generation {
			generation = n
		}
	}

	return generation, nil
}

// generationNote is what a rollback writes under the generation object: enough to read
// the rollback's story from the bucket alone.
type generationNote struct {
	Generation int    `json:"generation"`
	Database   string `json:"database"`
	Backup     string `json:"backup"`
	Kept       string `json:"kept"`
	Build      string `json:"build"`
	Release    string `json:"release"`
	Requester  string `json:"requester"`
	Approver   string `json:"approver,omitempty"`
	Reason     string `json:"reason"`
	At         string `json:"at"`
}

// restoreGeneration does a generation restore's work before the plan, as the apply
// identity: the chosen backup is read (or, for a moment, made as of it, of the live
// database or of the earlier generation the instruction names) and waited for until
// READY; a forensic backup of the live database is started as of now; the chosen backup
// is restored into the database's next generation, named after the current database's
// base with the number; the generation is written beside the deployment records; the
// restored database is imported into the stack as its generation's instance; and the
// facts carry the rest to the record. It answers the new generation, which the plan is
// told. The live database is never dropped and no earlier generation is put back into
// service.
func (s *stack) restoreGeneration(ctx context.Context, subs, facts map[string]string, w Workspace, generation int, now time.Time) (int, error) {
	app, env := subs[appSub], subs[envSub]
	if subs[recordsBucket] == "" {
		return 0, errors.Newf("%s carries no %s: a restore writes the database's generation beside the deployment records, so the run refuses before touching anything", BuildFile, recordsBucket)
	}
	b, err := w.Build()
	if err != nil {
		return 0, err
	}
	database, instance, err := currentDatabase(subs)
	if err != nil {
		return 0, err
	}
	base := databaseBase(path.Base(database), generation)
	next := generation + 1
	restored := base + "-" + strconv.Itoa(next)
	store, err := s.clients.SpannerAs(ctx, s.identity)
	if err != nil {
		return 0, err
	}
	// The chosen backup is found (or, for a moment, started) and waited for until READY
	// before the forensic backup starts: Spanner takes one backup of a database at a
	// time, and the chosen one is often the release backup the release build started
	// minutes ago, still being taken. The application is in maintenance meanwhile, so the
	// forensic backup, taken as of this moment, holds the live data as the restore found
	// it. A refusal of the chosen backup leaves no backup behind.
	of := database
	if named := facts[restoreSourceDatabaseFact]; named != "" && strings.HasPrefix(facts[restoreFact], restoreMomentPrefix) {
		of = named
		if !strings.Contains(of, "/") {
			of = instance + databaseDatabaseSegment + named
		}
	}
	backup, err := s.chosenBackup(ctx, store, instance, of, facts[restoreFact], now)
	if err != nil {
		return 0, err
	}
	if backup, err = readyBackup(ctx, s.clients, store, backup, s.out); err != nil {
		return 0, err
	}
	stamp := now.UTC().Format(backupStamp)
	forensic := path.Base(database) + forensicBackupInfix + stamp
	if _, err := startBackup(ctx, s.clients, s.out, store, instance, forensic, database, now.UTC(), now.UTC().Add(forensicBackupKeep), "the forensic backup "+forensic); err != nil {
		return 0, errors.Wrapf(err, "starting the forensic backup %s of %s", forensic, path.Base(database))
	}
	fmt.Fprintf(s.out, "Forensic backup: %s holds %s as of %s, kept thirty days; %s itself stays, protected, as the forensic copy. The backup stands as this run's release backup in the record, so a later restore to this release's last data finds it.\n", forensic, path.Base(database), now.UTC().Format(time.RFC3339), path.Base(database))
	fmt.Fprintf(s.out, "=== Restore: %s is restored from %s (data as of %s) into %s, generation %d of %s's database ===\n", env, path.Base(backup.Name), backup.VersionTime, restored, next, app)
	if err := store.RestoreDatabase(ctx, instance, restored, backup.Name); err != nil {
		return 0, errors.Wrapf(err, "restoring %s from %s", restored, backup.Name)
	}
	fmt.Fprintf(s.out, "Restored %s from %s; Spanner optimizes it in the background and it serves meanwhile.\n", restored, path.Base(backup.Name))
	note := generationNote{Generation: next, Database: instance + databaseDatabaseSegment + restored, Backup: backup.Name, Kept: database, Build: b.ID, Release: subs[tagSub], Requester: facts[requesterFact], Approver: facts[approverFact], Reason: facts[restoreReasonFact], At: now.UTC().Format(time.RFC3339)}
	if err := s.writeGeneration(ctx, subs[recordsBucket], app, env, &note); err != nil {
		return 0, err
	}
	address := fmt.Sprintf("google_spanner_database.restored[%q]", strconv.Itoa(next))
	if err := s.tofu(ctx, "import", "-input=false", "-no-color", varFlag, "environment="+env, varFlag, "database_generation="+strconv.Itoa(next), address, note.Database); err != nil {
		return 0, errors.Wrapf(err, "importing %s into the stack as %s", restored, address)
	}
	fmt.Fprintf(s.out, "Imported %s into the stack as %s; the plan points the stack at generation %d.\n", restored, address, next)
	stamp = now.UTC().Format(time.RFC3339)
	err = w.Append(map[string]string{
		backupFact: backup.Name, backupTimeFact: backup.VersionTime, restoreForensicFact: instance + databaseBackupsSegment + forensic,
		restoreIntoFact: note.Database, restoreKeptFact: database, previousGenerationFact: strconv.Itoa(generation),
		cutFact: stamp, releaseBackupFact: instance + databaseBackupsSegment + forensic, releaseBackupTimeFact: stamp, releaseBackupExpiresFact: now.UTC().Add(forensicBackupKeep).Format(time.RFC3339),
	})
	if err != nil {
		return 0, err
	}

	return next, nil
}

// chosenBackup is the backup the restore restores: the one the instruction names, read
// once, or one started as of the moment the instruction gives, of database (the live one,
// or the earlier generation the instruction names). There being no such backup, and a
// backup of another application's database, are refused.
func (s *stack) chosenBackup(ctx context.Context, store Spanner, instance, database, instruction string, now time.Time) (*Backup, error) {
	name := instruction
	if moment, ok := strings.CutPrefix(instruction, restoreMomentPrefix); ok {
		at, err := time.Parse(time.RFC3339, moment)
		if err != nil {
			return nil, errors.Newf("%s=%s: the moment is not RFC 3339", restoreSub, instruction)
		}
		id := path.Base(database) + pointInTimeBackupInfix + at.UTC().Format(backupStamp)
		if _, err := startBackup(ctx, s.clients, s.out, store, instance, id, database, at.UTC(), now.UTC().Add(pointInTimeBackupKeep), "the backup "+id+" as of "+at.UTC().Format(time.RFC3339)); err != nil {
			return nil, errors.Wrapf(err, "making the backup %s of %s as of %s", id, path.Base(database), at.UTC().Format(time.RFC3339))
		}
		name = instance + databaseBackupsSegment + id
		fmt.Fprintf(s.out, "Point in time: %s is made of %s as of %s, kept fourteen days; the restore waits for it.\n", id, path.Base(database), at.UTC().Format(time.RFC3339))
	}
	backup, err := store.Backup(ctx, name)
	if err != nil {
		return nil, errors.Wrapf(err, "reading the backup %s", name)
	}
	if backup == nil {
		return nil, errors.Newf("%s=%s: there is no such backup on %s", restoreSub, instruction, path.Base(instance))
	}
	if base := databaseBase(path.Base(database), 0); !strings.HasPrefix(path.Base(backup.Database), base) {
		return nil, errors.Newf("%s=%s is a backup of %s, not of this application's database (%s)", restoreSub, instruction, path.Base(backup.Database), base)
	}

	return backup, nil
}

// readyBackup is the backup once Spanner has finished taking it: a restore (a rollback's,
// or an environment's from production's backup) needs a READY backup, and a backup just
// started (a point in time's, or a release backup the release build started minutes ago)
// is CREATING for a while.
func readyBackup(ctx context.Context, clients *Clients, store Spanner, backup *Backup, out io.Writer) (*Backup, error) {
	waited := time.Duration(0)
	for backup.State != BackupReady {
		if waited == 0 || waited%backupWaitSays == 0 {
			fmt.Fprintf(out, "Waiting for %s: Spanner is still taking it (%s after %s); a restore needs a READY backup.\n", path.Base(backup.Name), backup.State, waited.Round(time.Minute))
		}
		if err := clients.sleep()(ctx, backupWaitPoll); err != nil {
			return nil, err
		}
		waited += backupWaitPoll
		again, err := store.Backup(ctx, backup.Name)
		if err != nil {
			return nil, errors.Wrapf(err, "reading the backup %s", backup.Name)
		}
		if again == nil {
			return nil, errors.Newf("the backup %s went away while the build waited for it", backup.Name)
		}
		backup = again
	}

	return backup, nil
}

// writeGeneration leaves the generation note beside the deployment records, as the deploy
// identity, which may create objects there and nothing else.
func (s *stack) writeGeneration(ctx context.Context, bucket, app, env string, note *generationNote) error {
	if bucket == "" {
		return errors.Newf("%s carries no %s: the generation is written beside the deployment records", BuildFile, recordsBucket)
	}
	store, err := s.clients.Storage(ctx)
	if err != nil {
		return err
	}
	defer store.Close()
	data, err := json.MarshalIndent(note, "", "  ")
	if err != nil {
		return errors.Wrap(err, "json.MarshalIndent()")
	}
	object := generationObject(app, env, note.Generation)
	if err := store.Write(ctx, bucket, object, append(data, '\n')); err != nil {
		return errors.Wrapf(err, "writing gs://%s/%s", bucket, object)
	}
	fmt.Fprintf(s.out, "Generation %d written: gs://%s/%s (every build after this one points the stack at it).\n", note.Generation, bucket, object)

	return nil
}

// currentDatabase is the Spanner database the triggers name for the migrate command (the
// generation the last apply pointed at), with its instance.
func currentDatabase(subs map[string]string) (database, instance string, err error) {
	raw := subs[migrateDatabasesSub]
	if raw == "" {
		return "", "", errors.Newf("%s carries no %s: the stack's triggers name the databases the migrate command reaches", BuildFile, migrateDatabasesSub)
	}
	var databases []string
	if err := json.Unmarshal([]byte(raw), &databases); err != nil {
		return "", "", errors.Wrapf(err, "json.Unmarshal(): %s", migrateDatabasesSub)
	}
	for _, d := range databases {
		if i := strings.Index(d, databaseDatabaseSegment); i > 0 && strings.Contains(d, "/instances/") {
			return d, d[:i], nil
		}
	}

	return "", "", errors.Newf("%s names no Spanner database", migrateDatabasesSub)
}

// databaseBase is the database's name without its generation: the name as given for
// generation 1 (or when the generation is unknown and the name ends in no number), else
// the name with "-<generation>" taken off its end.
func databaseBase(name string, generation int) string {
	if generation > 1 {
		return strings.TrimSuffix(name, "-"+strconv.Itoa(generation))
	}
	if generation == 0 {
		if i := strings.LastIndex(name, "-"); i > 0 {
			if n, err := strconv.Atoi(name[i+1:]); err == nil && n > 1 {
				return name[:i]
			}
		}
	}

	return name
}
