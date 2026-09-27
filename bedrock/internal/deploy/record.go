package deploy

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/go-playground/errors/v5"
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
	record := Record{
		App:       build.Substitutions[appSub],
		Env:       build.Substitutions[envSub],
		Version:   env[versionFact],
		Commit:    build.Substitutions[commitSub],
		Image:     env[imageFact] + "@" + env[digestFact],
		Digest:    env[digestFact],
		Regions:   regions,
		Revisions: revisions,
		Timestamp: now.UTC().Format(time.RFC3339),
		Status:    status,
		Build:     build.ID,
	}

	return &RecordRequest{
		Bucket: build.Substitutions[recordsBucket],
		Object: record.App + "/" + record.Env + "/" + env[releaseFact] + "/" + build.ID + ".json",
		Record: record,
	}, nil
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
