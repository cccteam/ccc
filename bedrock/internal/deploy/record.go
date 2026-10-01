package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/migration"
	"google.golang.org/api/iterator"
)

// Record is what a build leaves in the records bucket once it has deployed: what is
// running where, and whether traffic moved to it. The next environment's gate reads it
// (a release reaches an environment after it is live in the previous one) and the
// release check reuses the image it names.
type Record struct {
	App       string     `json:"app"`
	Env       string     `json:"env"`
	Version   string     `json:"version"`
	Commit    string     `json:"commit"`
	Image     string     `json:"image"`
	Digest    string     `json:"digest"`
	Regions   []string   `json:"regions"`
	Revisions []Revision `json:"revisions"`
	Timestamp string     `json:"timestamp"`
	// Status is live once traffic shifted to the revisions, preview otherwise (a
	// pull-request build, whose revision serves under a tag).
	Status string `json:"status"`
	Build  string `json:"build"`
	// Migrations lists the migration files the build applied (the schema migrations,
	// and the seed migrations where the seed ran), each with its content's hash: what
	// the database holds, so a later build of a pull request can tell that a file was
	// renumbered or changed since and recreate the database. Absent when no migration
	// ran.
	Migrations []Migration `json:"migrations,omitempty"`
	// Stack is what the tag build's apply of the environment's stack did (the plan it
	// applied, made in this build): its counts and changes. Absent in a pull-request
	// build, which applies no environment's stack, and on a bedrock before the step.
	Stack *StackPlan `json:"stack,omitempty"`
	// Restore says the build was a restore run: what the environment's database was
	// replaced with, who asked, and what the stack replaced. Absent otherwise.
	Restore *Restore `json:"restore,omitempty"`
}

// Restore is a restore run as the record keeps it.
type Restore struct {
	// Kind is the restore: empty (an empty database, filled by the migrations and the
	// seed) or production-backup.
	Kind      string `json:"kind"`
	Requester string `json:"requester"`
	// Replaced lists the stack's resources the run replaced, by address.
	Replaced []string `json:"replaced,omitempty"`
}

// Migration is one migration file a build applied: its directory (root-relative), its
// name and its content's hash (the first 16 hex digits of the SHA-256).
type Migration struct {
	Dir  string `json:"dir"`
	Name string `json:"name"`
	Hash string `json:"hash"`
}

// The two statuses a record carries.
const (
	Live    = "live"
	Preview = "preview"
)

// RecordRequest is what the record step found in the workspace: the record to write and
// where, or that there is nothing to record.
type RecordRequest struct {
	// Skipped says why nothing is recorded: the pull request's environment was torn down.
	Skipped string
	Bucket  string
	Object  string
	Record  Record
}

// The facts and substitutions the record reads.
const (
	skipDeploy    = "SKIP_DEPLOY"
	services      = "SERVICES"
	shiftTraffic  = "SHIFT_TRAFFIC"
	versionFact   = "VERSION"
	releaseFact   = "RELEASE"
	imageFact     = "IMAGE"
	digestFact    = "IMAGE_DIGEST"
	appSub        = "_APP"
	envSub        = "_ENV"
	recordsBucket = "_RECORDS_BUCKET"
	commitSub     = "COMMIT_SHA"
	migrationsSub = "_MIGRATIONS_DIR"
)

// NewRecordRequest composes the record from the workspace: the facts the resolve step
// exported (and the image build appended), the build's substitutions, and the revisions
// the deploy step created. now stamps it.
func NewRecordRequest(w Workspace, now time.Time) (*RecordRequest, error) {
	env, err := w.Environment()
	if err != nil {
		return nil, err
	}
	if env[skipDeploy] == trueValue {
		return &RecordRequest{Skipped: "The pull request's environment was torn down: nothing to record."}, nil
	}
	build, err := w.Build()
	if err != nil {
		return nil, err
	}
	for _, name := range []string{services, versionFact, releaseFact, imageFact, digestFact} {
		if env[name] == "" {
			return nil, errors.Newf("%s exports no %s: the steps before this one did not run, or ran out of order", EnvironmentFile, name)
		}
	}
	for _, name := range []string{appSub, envSub, recordsBucket, commitSub} {
		if build.Substitutions[name] == "" {
			return nil, errors.Newf("%s carries no substitution %s", BuildFile, name)
		}
	}
	revisions, err := w.Revisions()
	if err != nil {
		return nil, err
	}
	status := Preview
	if env[shiftTraffic] == trueValue {
		status = Live
	}
	var regions []string
	for _, pair := range strings.Split(env[services], ",") {
		region, _, _ := strings.Cut(pair, "=")
		regions = append(regions, region)
	}
	var applied []Migration
	if env[runMigrationsFact] == trueValue {
		dirs := []string{build.Substitutions[migrationsSub]}
		if build.Substitutions[seedSub] == trueValue {
			dirs = append(dirs, path.Join(path.Dir(dirs[0]), derive.SeedDir))
		}
		if applied, err = listMigrations(string(w), dirs); err != nil {
			return nil, err
		}
	}
	stack, err := stackPlanOf(w)
	if err != nil {
		return nil, err
	}
	var restore *Restore
	if env[restoreFact] != "" {
		restore = &Restore{Kind: env[restoreFact], Requester: env[requesterFact]}
		if env[restoredFact] != "" {
			restore.Replaced = strings.Split(env[restoredFact], ",")
		}
	}
	record := Record{
		App:        build.Substitutions[appSub],
		Env:        build.Substitutions[envSub],
		Version:    env[versionFact],
		Commit:     build.Substitutions[commitSub],
		Image:      env[imageFact] + "@" + env[digestFact],
		Digest:     env[digestFact],
		Regions:    regions,
		Revisions:  revisions,
		Timestamp:  now.UTC().Format(time.RFC3339),
		Status:     status,
		Build:      build.ID,
		Migrations: applied,
		Stack:      stack,
		Restore:    restore,
	}

	return &RecordRequest{
		Bucket: build.Substitutions[recordsBucket],
		Object: record.App + "/" + record.Env + "/" + env[releaseFact] + "/" + build.ID + ".json",
		Record: record,
	}, nil
}

// listMigrations reads the migration files under each directory of the checkout, in
// order, with their hashes; a directory that does not exist, or an empty one, lists
// nothing. Nothing is listed for an empty directory name.
func listMigrations(root string, dirs []string) ([]Migration, error) {
	var applied []Migration
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}

			return nil, errors.Wrapf(err, "os.ReadDir(): %s", dir)
		}
		for _, e := range entries {
			if e.IsDir() || !migration.NameRE.MatchString(e.Name()) {
				continue
			}
			hash, err := hashFile(filepath.Join(root, filepath.FromSlash(dir), e.Name()))
			if err != nil {
				return nil, err
			}
			applied = append(applied, Migration{Dir: dir, Name: e.Name(), Hash: hash})
		}
	}

	return applied, nil
}

// hashFile is the file's content hash as the record carries it; empty when the file
// does not exist.
func hashFile(name string) (string, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}

		return "", errors.Wrapf(err, "os.ReadFile(): %s", name)
	}
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:8]), nil
}

// JSON is the record as written: indented, the way a person reads it in the bucket.
func (r *Record) JSON() ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, errors.Wrap(err, "json.MarshalIndent()")
	}

	return append(data, '\n'), nil
}

// Store writes objects into buckets: Cloud Storage, or a fake in tests.
type Store interface {
	Write(ctx context.Context, bucket, object string, data []byte) error
	// List names the objects under the prefix; Read is one object's content.
	List(ctx context.Context, bucket, prefix string) ([]string, error)
	Read(ctx context.Context, bucket, object string) ([]byte, error)
	Close() error
}

// StoreFunc opens a Store.
type StoreFunc func(ctx context.Context) (Store, error)

// NewStorage opens Cloud Storage with the process's default credentials (in Cloud Build,
// the build's service account).
func NewStorage(ctx context.Context) (Store, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "storage.NewClient()")
	}

	return &cloudStorage{client: client}, nil
}

// cloudStorage is Store over the Cloud Storage client.
type cloudStorage struct {
	client *storage.Client
}

func (s *cloudStorage) List(ctx context.Context, bucket, prefix string) ([]string, error) {
	var names []string
	it := s.client.Bucket(bucket).Objects(ctx, &storage.Query{Prefix: prefix})
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return names, nil
		}
		if err != nil {
			return nil, errors.Wrapf(err, "storage.ObjectIterator.Next(): gs://%s/%s", bucket, prefix)
		}
		names = append(names, attrs.Name)
	}
}

func (s *cloudStorage) Read(ctx context.Context, bucket, object string) ([]byte, error) {
	r, err := s.client.Bucket(bucket).Object(object).NewReader(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "storage.ObjectHandle.NewReader(): gs://%s/%s", bucket, object)
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, errors.Wrapf(err, "io.ReadAll(): gs://%s/%s", bucket, object)
	}

	return data, nil
}

func (s *cloudStorage) Write(ctx context.Context, bucket, object string, data []byte) error {
	w := s.client.Bucket(bucket).Object(object).NewWriter(ctx)
	w.ContentType = "application/json"
	if _, err := w.Write(data); err != nil {
		_ = w.Close()

		return errors.Wrapf(err, "storage.Writer.Write(): gs://%s/%s", bucket, object)
	}
	if err := w.Close(); err != nil {
		return errors.Wrapf(err, "storage.Writer.Close(): gs://%s/%s", bucket, object)
	}

	return nil
}

func (s *cloudStorage) Close() error {
	if err := s.client.Close(); err != nil {
		return errors.Wrap(err, "storage.Client.Close()")
	}

	return nil
}

// WriteRecord writes the request's record to its object and prints it, the way the
// bash step did, with where it went.
func WriteRecord(ctx context.Context, open StoreFunc, req *RecordRequest, out io.Writer) error {
	if req.Skipped != "" {
		if _, err := io.WriteString(out, req.Skipped+"\n"); err != nil {
			return errors.Wrap(err, "io.WriteString()")
		}

		return nil
	}
	data, err := req.Record.JSON()
	if err != nil {
		return err
	}
	store, err := open(ctx)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := store.Write(ctx, req.Bucket, req.Object, data); err != nil {
		return err
	}
	if _, err := io.WriteString(out, string(data)+"Recorded "+req.Record.Status+" deployment of "+req.Record.Version+" in "+req.Record.Env+": gs://"+req.Bucket+"/"+req.Object+"\n"); err != nil {
		return errors.Wrap(err, "io.WriteString()")
	}

	return nil
}

// stackPlanOf reads the plan the tag build applied (deploy stack plan writes its JSON to
// the workspace), or nil when the build applied none.
func stackPlanOf(w Workspace) (*StackPlan, error) {
	data, err := os.ReadFile(filepath.Join(string(w), StackPlanJSONFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrapf(err, "os.ReadFile(): %s", StackPlanJSONFile)
	}

	return stackPlan(data)
}
