// rollback.go is the rollback run: bedrock rollback runs the environment's rollback trigger
// at the release the environment returns to, and the pipeline, under the instruction, keeps
// the live database as the forensic copy (a backup of it is taken as of now), restores the
// chosen backup into the database's next generation, writes the generation beside the
// deployment records as the fact every later build reads, adopts the restored database into
// the stack and points the stack at it, so the release's migrations, its revision and its
// traffic land on the restored data. Nothing is dropped.

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

// The facts a rollback run leaves beyond the instruction's: the backup it restored (named,
// after a point-in-time backup was made), the moment its data is from, the forensic backup
// of the live database, the database restored into and the one kept, and the generation
// before this run's.
const (
	rollbackBackupFact      = "ROLLBACK_BACKUP"
	rollbackBackupTimeFact  = "ROLLBACK_BACKUP_TIME"
	rollbackForensicFact    = "ROLLBACK_FORENSIC"
	rollbackDatabaseFact    = "ROLLBACK_DATABASE"
	rollbackKeptFact        = "ROLLBACK_KEPT"
	previousGenerationFact  = "DATABASE_PREVIOUS_GENERATION"
	rollbackMomentPrefix    = "@"
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

// rollback reads the rollback instruction: the rollback trigger alone carries _ROLLBACK, a
// backup's resource name or @<moment> (RFC 3339), with the release the environment leaves
// (_ROLLBACK_FROM), who asked (_REQUESTER) and why (_REASON); a pull-request build carries
// none, and a rollback goes with no restore and no migration operation.
func (f *Facts) rollback() error {
	backup, from, reason, requester := f.Substitutions[rollbackSub], f.Substitutions[rollbackFromSub], f.Substitutions[reasonSub], f.Substitutions[requesterSub]
	if backup == "" {
		if from != "" || reason != "" {
			return errors.Newf("%s or %s without %s: a rollback names the backup it restores (bedrock rollback sets all three on the rollback trigger)", rollbackFromSub, reasonSub, rollbackSub)
		}

		return nil
	}
	switch {
	case f.Tag == "":
		return errors.Newf("%s=%s on a pull-request build: a rollback is a release build's instruction, run by bedrock rollback on the environment's rollback trigger", rollbackSub, backup)
	case f.Restore != "":
		return errors.Newf("%s=%s with %s=%s: a rollback restores a backup into the database's next generation and a restore replaces the database; a run does one", rollbackSub, backup, restoreSub, f.Restore)
	case f.Substitutions[migrateActionSub] != "":
		return errors.Newf("%s=%s with %s=%s: a rollback runs the release's migrations on the restored database; there is no migration state to operate on", rollbackSub, backup, migrateActionSub, f.Substitutions[migrateActionSub])
	case requester == "":
		return errors.Newf("%s=%s names no requester (%s): a rollback says who asked for it", rollbackSub, backup, requesterSub)
	case reason == "":
		return errors.Newf("%s=%s gives no reason (%s): a rollback says why it was asked for", rollbackSub, backup, reasonSub)
	case from == "":
		return errors.Newf("%s=%s names no release it leaves (%s): the environment's live release when the rollback was asked for", rollbackSub, backup, rollbackFromSub)
	case !releaseTagRE.MatchString(from):
		return errors.Newf("%s=%q is not a release tag (v<major>.<minor>.<patch>)", rollbackFromSub, from)
	}
	if moment, ok := strings.CutPrefix(backup, rollbackMomentPrefix); ok {
		if _, err := time.Parse(time.RFC3339, moment); err != nil {
			return errors.Newf("%s=%s: the moment after @ is not RFC 3339 (2026-10-05T04:30:00Z)", rollbackSub, backup)
		}
	} else if !strings.Contains(backup, databaseBackupsSegment) || !strings.HasPrefix(backup, "projects/") {
		return errors.Newf("%s=%q is neither a backup's resource name (projects/<p>/instances/<i>/backups/<b>) nor @<moment>", rollbackSub, backup)
	}
	f.Rollback, f.RollbackFrom, f.RollbackReason, f.Requester = backup, from, reason, requester

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
	data := "the backup " + path.Base(f.Rollback)
	if moment, ok := strings.CutPrefix(f.Rollback, rollbackMomentPrefix); ok {
		data = "a backup made as of " + moment
	}
	approved := ""
	if f.Approver != "" {
		approved = ", approved by " + f.Approver
	}
	fmt.Fprintf(out, "=== ROLLBACK of %s: %s returns to %s from %s, asked for by %s%s: %s ===\n", f.Environment, f.Substitutions[appSub], f.Tag, f.RollbackFrom, f.Requester, approved, f.RollbackReason)
	fmt.Fprintf(out, "The application goes into maintenance. The live database is kept as the forensic copy and a backup of it is taken as of now. %s is restored into the database's next generation; %s's migrations run on it (nothing applies when the backup is at %s's schema); %s deploys and takes the traffic; the record names all of it. Writes made after the backup's moment are in the forensic copy alone.\n", data, f.Tag, f.Tag, f.Tag)
}

// Rollback is a rollback run as the record keeps it.
type Rollback struct {
	Requester string `json:"requester"`
	Reason    string `json:"reason"`
	// From is the release the environment left, live when the rollback was asked for.
	From string `json:"from"`
	// Backup is the backup restored and BackupTime the moment its data is from; Forensic
	// the backup taken of the live database as the rollback began.
	Backup     string `json:"backup"`
	BackupTime string `json:"backupTime,omitempty"`
	Forensic   string `json:"forensic,omitempty"`
	// Database is the database the backup was restored into (the current generation) and
	// Kept the one left as the forensic copy; Generation and PreviousGeneration their
	// numbers.
	Database           string `json:"database"`
	Kept               string `json:"kept"`
	Generation         int    `json:"generation"`
	PreviousGeneration int    `json:"previousGeneration"`
}

// rollbackOf reads a rollback run's note from its facts; nil for any other run.
func rollbackOf(env map[string]string) *Rollback {
	if env[rollbackFact] == "" {
		return nil
	}
	r := &Rollback{Requester: env[requesterFact], Reason: env[rollbackReasonFact], From: env[rollbackFromFact], Backup: env[rollbackBackupFact], BackupTime: env[rollbackBackupTimeFact], Forensic: env[rollbackForensicFact], Database: env[rollbackDatabaseFact], Kept: env[rollbackKeptFact]}
	if r.Backup == "" {
		r.Backup = env[rollbackFact]
	}
	r.Generation, _ = strconv.Atoi(env[databaseGenerationFact])
	r.PreviousGeneration, _ = strconv.Atoi(env[previousGenerationFact])

	return r
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
	From       string `json:"from"`
	Requester  string `json:"requester"`
	Approver   string `json:"approver,omitempty"`
	Reason     string `json:"reason"`
	At         string `json:"at"`
}

// rollback does a rollback run's work before the plan, as the apply identity: the chosen
// backup is read (or, for a moment, made as of it) and waited for until READY; a forensic
// backup of the live database is started as of now; the chosen backup is restored into the
// database's next generation, named after the current database's base with the number;
// the generation is written beside the deployment records; the restored database is
// imported into the stack as its generation's instance; and the facts carry the rest to
// the record. It answers the new generation, which the plan is told.
func (s *stack) rollback(ctx context.Context, subs, facts map[string]string, w Workspace, generation int, now time.Time) (int, error) {
	app, env := subs[appSub], subs[envSub]
	if subs[recordsBucket] == "" {
		return 0, errors.Newf("%s carries no %s: a rollback writes the database's generation beside the deployment records, so the run refuses before touching anything", BuildFile, recordsBucket)
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
	// forensic backup, taken as of this moment, holds the live data as the rollback found
	// it. A refusal of the chosen backup leaves no backup behind.
	backup, err := s.chosenBackup(ctx, store, instance, database, facts[rollbackFact], now)
	if err != nil {
		return 0, err
	}
	if backup, err = s.readyBackup(ctx, store, backup); err != nil {
		return 0, err
	}
	stamp := now.UTC().Format(backupStamp)
	forensic := path.Base(database) + forensicBackupInfix + stamp
	if _, err := startBackup(ctx, s.clients, s.out, store, instance, forensic, database, now.UTC(), now.UTC().Add(forensicBackupKeep), "the forensic backup "+forensic); err != nil {
		return 0, errors.Wrapf(err, "starting the forensic backup %s of %s", forensic, path.Base(database))
	}
	fmt.Fprintf(s.out, "Forensic backup: %s holds %s as of %s, kept thirty days; %s itself stays, protected, as the forensic copy. The backup stands as this run's release backup in the record, so a later rollback from this release finds its last data there.\n", forensic, path.Base(database), now.UTC().Format(time.RFC3339), path.Base(database))
	fmt.Fprintf(s.out, "=== Rollback: %s is restored from %s (data as of %s) into %s, generation %d of %s's database ===\n", env, path.Base(backup.Name), backup.VersionTime, restored, next, app)
	if err := store.RestoreDatabase(ctx, instance, restored, backup.Name); err != nil {
		return 0, errors.Wrapf(err, "restoring %s from %s", restored, backup.Name)
	}
	fmt.Fprintf(s.out, "Restored %s from %s; Spanner optimizes it in the background and it serves meanwhile.\n", restored, path.Base(backup.Name))
	note := generationNote{Generation: next, Database: instance + databaseDatabaseSegment + restored, Backup: backup.Name, Kept: database, Build: b.ID, Release: subs[tagSub], From: facts[rollbackFromFact], Requester: facts[requesterFact], Approver: facts[approverFact], Reason: facts[rollbackReasonFact], At: now.UTC().Format(time.RFC3339)}
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
		rollbackBackupFact: backup.Name, rollbackBackupTimeFact: backup.VersionTime, rollbackForensicFact: instance + databaseBackupsSegment + forensic,
		rollbackDatabaseFact: note.Database, rollbackKeptFact: database, previousGenerationFact: strconv.Itoa(generation),
		cutFact: stamp, releaseBackupFact: instance + databaseBackupsSegment + forensic, releaseBackupTimeFact: stamp, releaseBackupExpiresFact: now.UTC().Add(forensicBackupKeep).Format(time.RFC3339),
	})
	if err != nil {
		return 0, err
	}

	return next, nil
}

// chosenBackup is the backup the rollback restores: the one the instruction names, read
// once, or one started as of the moment the instruction gives. There being no such
// backup, and a backup of another application's database, are refused.
func (s *stack) chosenBackup(ctx context.Context, store Spanner, instance, database, instruction string, now time.Time) (*Backup, error) {
	name := instruction
	if moment, ok := strings.CutPrefix(instruction, rollbackMomentPrefix); ok {
		at, err := time.Parse(time.RFC3339, moment)
		if err != nil {
			return nil, errors.Newf("%s=%s: the moment is not RFC 3339", rollbackSub, instruction)
		}
		id := path.Base(database) + pointInTimeBackupInfix + at.UTC().Format(backupStamp)
		if _, err := startBackup(ctx, s.clients, s.out, store, instance, id, database, at.UTC(), now.UTC().Add(pointInTimeBackupKeep), "the backup "+id+" as of "+at.UTC().Format(time.RFC3339)); err != nil {
			return nil, errors.Wrapf(err, "making the backup %s of %s as of %s", id, path.Base(database), at.UTC().Format(time.RFC3339))
		}
		name = instance + databaseBackupsSegment + id
		fmt.Fprintf(s.out, "Point in time: %s is made as of %s, kept fourteen days; the rollback waits for it.\n", id, at.UTC().Format(time.RFC3339))
	}
	backup, err := store.Backup(ctx, name)
	if err != nil {
		return nil, errors.Wrapf(err, "reading the backup %s", name)
	}
	if backup == nil {
		return nil, errors.Newf("%s=%s: there is no such backup on %s", rollbackSub, instruction, path.Base(instance))
	}
	if base := databaseBase(path.Base(database), 0); !strings.HasPrefix(path.Base(backup.Database), base) {
		return nil, errors.Newf("%s=%s is a backup of %s, not of this application's database (%s)", rollbackSub, instruction, path.Base(backup.Database), base)
	}

	return backup, nil
}

// readyBackup is the backup once Spanner has finished taking it: a restore needs a READY
// backup, and a backup just started (a point in time's, or a release backup the release
// build started minutes ago) is CREATING for a while.
func (s *stack) readyBackup(ctx context.Context, store Spanner, backup *Backup) (*Backup, error) {
	waited := time.Duration(0)
	for backup.State != BackupReady {
		if waited == 0 || waited%backupWaitSays == 0 {
			fmt.Fprintf(s.out, "Waiting for %s: Spanner is still taking it (%s after %s); a restore needs a READY backup.\n", path.Base(backup.Name), backup.State, waited.Round(time.Minute))
		}
		if err := s.clients.sleep()(ctx, backupWaitPoll); err != nil {
			return nil, err
		}
		waited += backupWaitPoll
		again, err := store.Backup(ctx, backup.Name)
		if err != nil {
			return nil, errors.Wrapf(err, "reading the backup %s", backup.Name)
		}
		if again == nil {
			return nil, errors.Newf("the backup %s went away while the rollback waited for it", backup.Name)
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
