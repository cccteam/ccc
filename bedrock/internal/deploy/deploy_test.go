package deploy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
)

func TestEnvironment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		file    string
		want    map[string]string
		wantErr string
	}{
		{name: "double quotes, with the shell's escapes", file: "export A=\"x\"\nexport B=\"a \\\"b\\\" \\$c \\\\d\"\n", want: map[string]string{"A": "x", "B": `a "b" $c \d`}},
		{name: "single quotes, a quote inside as the shell writes it", file: "export A='plain'\nexport B='it'\\''s'\n", want: map[string]string{"A": "plain", "B": "it's"}},
		{name: "bare values and other lines ignored", file: "# a comment\nset -e\nexport A=bare\nexport EMPTY=\nB=notexported\n", want: map[string]string{"A": "bare", "EMPTY": ""}},
		{name: "a later export replaces an earlier one, as the shell would", file: "export A=\"1\"\nexport A=\"2\"\n", want: map[string]string{"A": "2"}},
		{name: "an unterminated quote is refused", file: "export A=\"open\n", wantErr: "unterminated double quote"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, EnvironmentFile), []byte(tt.file), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := Workspace(dir).Environment()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Environment() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Environment() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Errorf("Environment() = %v, want %v", got, tt.want)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("Environment()[%s] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

// workspaceFiles is a workspace's files by name, written into a temporary directory.
func workspaceFiles(t *testing.T, files map[string]string) Workspace {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, filepath.FromSlash(name))), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return Workspace(dir)
}

const (
	liveEnvironment = "export SERVICES=\"us-central1=harbor-app,us-west3=harbor-app\"\nexport SHIFT_TRAFFIC=\"true\"\nexport VERSION=\"v1.2.3\"\nexport RELEASE=\"v1.2.3\"\nexport IMAGE=\"us-central1-docker.pkg.dev/shr/repo/harbor\"\nexport SKIP_DEPLOY=\"\"\nexport IMAGE_DIGEST=\"sha256:abc\"\n"
	buildJSON       = `{"id": "b-1", "substitutions": {"_APP": "harbor", "_ENV": "tst", "_RECORDS_BUCKET": "records", "COMMIT_SHA": "deadbeef"}}`
	revisionsLines  = "us-central1,harbor-app,harbor-app-00007-abc\nus-west3,harbor-app,harbor-app-00007-def\n"
)

func TestNewRecordRequest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 27, 5, 30, 0, 0, time.UTC)
	tests := []struct {
		name        string
		files       map[string]string
		wantSkipped string
		wantObject  string
		wantStatus  string
		wantRegions string
		// wantMigrations is the applied listing a build that ran migrations leaves;
		// wantStack the plan a tag build applied.
		wantMigrations []Migration
		wantStack      *StackPlan
		// wantRestore is the restore note a restore run leaves; wantMaintenance the
		// maintenance the run went through.
		wantRestore     *Restore
		wantMaintenance *Maintenance
		wantErr         string
	}{
		{
			name:            "a run in maintenance records its revisions, queue, canceled executions and wait",
			files:           map[string]string{EnvironmentFile: liveEnvironment + "export MAINTENANCE=\"true\"\nexport MAINTENANCE_REVISIONS=\"us-central1=harbor-app-00008-maint,us-west3=harbor-app-00008-maint\"\nexport MAINTENANCE_QUEUE=\"projects/p/locations/us-central1/queues/harbor-tasks\"\nexport MAINTENANCE_PURGED=\"true\"\nexport MAINTENANCE_CANCELED=\"2\"\nexport MAINTENANCE_WAITED=\"4s: no active instance\"\n", BuildFile: buildJSON, RevisionsFile: revisionsLines},
			wantObject:      "harbor/tst/v1.2.3/b-1.json",
			wantStatus:      Live,
			wantRegions:     "us-central1,us-west3",
			wantMaintenance: &Maintenance{Revisions: map[string]string{"us-central1": "harbor-app-00008-maint", "us-west3": "harbor-app-00008-maint"}, Queue: "projects/p/locations/us-central1/queues/harbor-tasks", Purged: true, Canceled: 2, Waited: "4s: no active instance"},
		},
		{
			name:        "a restore run records what replaced the database and who asked",
			files:       map[string]string{EnvironmentFile: liveEnvironment + "export RESTORE=\"empty\"\nexport RESTORE_REQUESTER=\"octocat\"\nexport RESTORE_REPLACED=\"google_spanner_database.harbor[0],google_storage_bucket.assets\"\nexport RESTORE_CLEARED=\"google_firestore_database.firestore\"\nexport RESTORE_BACKUP=\"projects/p/instances/i/backups/b-20261001\"\nexport RESTORE_BACKUP_TIME=\"2026-10-01T02:00:00Z\"\n", BuildFile: buildJSON, RevisionsFile: revisionsLines},
			wantObject:  "harbor/tst/v1.2.3/b-1.json",
			wantStatus:  Live,
			wantRegions: "us-central1,us-west3",
			wantRestore: &Restore{Kind: "empty", Requester: "octocat", Replaced: []string{"google_spanner_database.harbor[0]", "google_storage_bucket.assets"}, Cleared: []string{"google_firestore_database.firestore"}, Backup: "projects/p/instances/i/backups/b-20261001", BackupTime: "2026-10-01T02:00:00Z"},
		},
		{
			name:        "a release that restored a seeded environment on its own records the reason beside the restore",
			files:       map[string]string{EnvironmentFile: liveEnvironment + "export RESTORE=\"empty\"\nexport RESTORE_REQUESTER=\"release v1.2.3\"\nexport RESTORE_REASON=\"the seed changed since v1.2.2 applied it (build b-0): schema/devseed/000001_Seed.up.sql, not in the tree as applied (edited, renumbered or removed since), so the database is recreated and the migrations and the seed apply from the start\"\nexport RESTORE_REPLACED=\"google_spanner_database.harbor[0]\"\n", BuildFile: buildJSON, RevisionsFile: revisionsLines},
			wantObject:  "harbor/tst/v1.2.3/b-1.json",
			wantStatus:  Live,
			wantRegions: "us-central1,us-west3",
			wantRestore: &Restore{Kind: "empty", Requester: "release v1.2.3", Reason: "the seed changed since v1.2.2 applied it (build b-0): schema/devseed/000001_Seed.up.sql, not in the tree as applied (edited, renumbered or removed since), so the database is recreated and the migrations and the seed apply from the start", Replaced: []string{"google_spanner_database.harbor[0]"}},
		},
		{
			name: "a build that ran migrations lists what it applied, the seed included when it ran",
			files: map[string]string{
				EnvironmentFile: liveEnvironment + "export RUN_MIGRATIONS=\"true\"\n", BuildFile: `{"id": "b-1", "substitutions": {"_APP": "harbor", "_ENV": "tst", "_RECORDS_BUCKET": "records", "COMMIT_SHA": "deadbeef", "_MIGRATIONS_DIR": "schema/migrations", "_SEED": "true"}}`, RevisionsFile: revisionsLines,
				"schema/migrations/000001_Init.up.sql": "create table a", "schema/migrations/000001_Init.down.sql": "drop table a", "schema/migrations/notes.txt": "not a migration", "schema/devseed/000001_Marker.up.sql": "insert marker",
			},
			wantObject:  "harbor/tst/v1.2.3/b-1.json",
			wantStatus:  Live,
			wantRegions: "us-central1,us-west3",
			wantMigrations: []Migration{
				{Dir: "schema/migrations", Name: "000001_Init.down.sql", Hash: hashOf("drop table a")},
				{Dir: "schema/migrations", Name: "000001_Init.up.sql", Hash: hashOf("create table a")},
				{Dir: "schema/devseed", Name: "000001_Marker.up.sql", Hash: hashOf("insert marker")},
			},
		},
		{
			name:           "a build that ran the schema migrations alone leaves the seed out",
			files:          map[string]string{EnvironmentFile: liveEnvironment + "export RUN_MIGRATIONS=\"true\"\n", BuildFile: `{"id": "b-1", "substitutions": {"_APP": "harbor", "_ENV": "tst", "_RECORDS_BUCKET": "records", "COMMIT_SHA": "deadbeef", "_MIGRATIONS_DIR": "schema/migrations", "_SEED": "false"}}`, RevisionsFile: revisionsLines, "schema/migrations/000001_Init.up.sql": "create table a", "schema/devseed/000001_Marker.up.sql": "insert marker"},
			wantObject:     "harbor/tst/v1.2.3/b-1.json",
			wantStatus:     Live,
			wantRegions:    "us-central1,us-west3",
			wantMigrations: []Migration{{Dir: "schema/migrations", Name: "000001_Init.up.sql", Hash: hashOf("create table a")}},
		},
		{
			name:        "a live deployment in two regions",
			files:       map[string]string{EnvironmentFile: liveEnvironment, BuildFile: buildJSON, RevisionsFile: revisionsLines},
			wantObject:  "harbor/tst/v1.2.3/b-1.json",
			wantStatus:  Live,
			wantRegions: "us-central1,us-west3",
		},
		{
			name:        "a tag build's apply of the environment's stack is recorded from its saved plan",
			files:       map[string]string{EnvironmentFile: liveEnvironment, BuildFile: buildJSON, RevisionsFile: revisionsLines, StackPlanJSONFile: `{"resource_changes": [{"address": "google_storage_bucket.assets", "type": "google_storage_bucket", "change": {"actions": ["create"], "after": {}}}]}`},
			wantObject:  "harbor/tst/v1.2.3/b-1.json",
			wantStatus:  Live,
			wantRegions: "us-central1,us-west3",
			wantStack:   &StackPlan{Add: 1, Changes: []StackChange{{Address: "google_storage_bucket.assets", Actions: []string{actionCreate}}}},
		},
		{
			name:        "a pull request's revision under its tag is a preview",
			files:       map[string]string{EnvironmentFile: strings.Replace(liveEnvironment, `SHIFT_TRAFFIC="true"`, `SHIFT_TRAFFIC="false"`, 1) + "export RELEASE=\"pr7-deadbee\"\n", BuildFile: buildJSON, RevisionsFile: revisionsLines},
			wantObject:  "harbor/tst/pr7-deadbee/b-1.json",
			wantStatus:  Preview,
			wantRegions: "us-central1,us-west3",
		},
		{
			name:        "a torn-down environment records nothing",
			files:       map[string]string{EnvironmentFile: "export SKIP_DEPLOY=\"true\"\n"},
			wantSkipped: "The pull request's environment was torn down: nothing to record.",
		},
		{
			name:    "a fact the earlier steps did not leave is refused",
			files:   map[string]string{EnvironmentFile: strings.Replace(liveEnvironment, "export IMAGE_DIGEST=\"sha256:abc\"\n", "", 1), BuildFile: buildJSON, RevisionsFile: revisionsLines},
			wantErr: "environment.sh exports no IMAGE_DIGEST",
		},
		{
			name:    "a substitution the build lacks is refused",
			files:   map[string]string{EnvironmentFile: liveEnvironment, BuildFile: `{"id": "b-1", "substitutions": {"_APP": "harbor", "_ENV": "tst", "COMMIT_SHA": "d"}}`, RevisionsFile: revisionsLines},
			wantErr: "build.json carries no substitution _RECORDS_BUCKET",
		},
		{
			name:    "a revisions line of the wrong shape is refused",
			files:   map[string]string{EnvironmentFile: liveEnvironment, BuildFile: buildJSON, RevisionsFile: "us-central1,harbor-app\n"},
			wantErr: `revisions.txt: "us-central1,harbor-app" is not region,service,revision`,
		},
		{
			name:    "a build without an id is refused",
			files:   map[string]string{EnvironmentFile: liveEnvironment, BuildFile: `{"substitutions": {}}`, RevisionsFile: revisionsLines},
			wantErr: "build.json names no build id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req, err := NewRecordRequest(workspaceFiles(t, tt.files), now)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("NewRecordRequest() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("NewRecordRequest() error = %v", err)
			}
			if req.Skipped != tt.wantSkipped {
				t.Errorf("Skipped = %q, want %q", req.Skipped, tt.wantSkipped)
			}
			if tt.wantSkipped != "" {
				return
			}
			if req.Bucket != "records" || req.Object != tt.wantObject {
				t.Errorf("gs://%s/%s, want gs://records/%s", req.Bucket, req.Object, tt.wantObject)
			}
			r := req.Record
			if r.Status != tt.wantStatus || strings.Join(r.Regions, ",") != tt.wantRegions || r.Image != "us-central1-docker.pkg.dev/shr/repo/harbor@sha256:abc" || r.Digest != "sha256:abc" || r.Commit != "deadbeef" || r.Build != "b-1" || r.Timestamp != "2026-09-27T05:30:00Z" || len(r.Revisions) != 2 || r.Revisions[1].Revision != "harbor-app-00007-def" {
				t.Errorf("Record = %+v", r)
			}
			if diff := cmp.Diff(tt.wantMigrations, r.Migrations); diff != "" {
				t.Errorf("Migrations mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantStack, r.Stack); diff != "" {
				t.Errorf("Stack mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantMaintenance, r.Maintenance); diff != "" {
				t.Errorf("Maintenance mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantRestore, r.Restore); diff != "" {
				t.Errorf("Restore mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// hashOf is a migration's hash as the record carries it.
func hashOf(content string) string {
	sum := sha256.Sum256([]byte(content))

	return hex.EncodeToString(sum[:8])
}

// memoryStore is a Store holding what was written, by gs:// path.
type memoryStore struct {
	objects map[string]string
	fail    error
}

func (m *memoryStore) open(context.Context) (Store, error) {
	return m, nil
}

func (m *memoryStore) Write(_ context.Context, bucket, object string, data []byte) error {
	if m.fail != nil {
		return m.fail
	}
	m.objects["gs://"+bucket+"/"+object] = string(data)

	return nil
}

func (m *memoryStore) List(_ context.Context, bucket, prefix string) ([]string, error) {
	var names []string
	for path := range m.objects {
		if object, ok := strings.CutPrefix(path, "gs://"+bucket+"/"); ok && strings.HasPrefix(object, prefix) {
			names = append(names, object)
		}
	}
	sort.Strings(names)

	return names, nil
}

func (m *memoryStore) Read(_ context.Context, bucket, object string) ([]byte, error) {
	data, ok := m.objects["gs://"+bucket+"/"+object]
	if !ok {
		return nil, errors.Newf("gs://%s/%s: no such object", bucket, object)
	}

	return []byte(data), nil
}

func (*memoryStore) Close() error {
	return nil
}

func TestWriteRecord(t *testing.T) {
	t.Parallel()

	record := Record{App: "harbor", Env: "tst", Version: "v1.2.3", Commit: "d", Image: "i@sha256:a", Digest: "sha256:a", Regions: []string{"us-central1"}, Revisions: []Revision{{Region: "us-central1", Service: "harbor-app", Revision: "r-1"}}, Timestamp: "2026-09-27T05:30:00Z", Status: Live, Build: "b-1"}
	tests := []struct {
		name       string
		req        *RecordRequest
		fail       error
		wantOut    []string
		wantStored string
		wantErr    string
	}{
		{
			name:       "the record is written and printed with where it went",
			req:        &RecordRequest{Bucket: "records", Object: "harbor/tst/v1.2.3/b-1.json", Record: record},
			wantOut:    []string{`"status": "live"`, "Recorded live deployment of v1.2.3 in tst: gs://records/harbor/tst/v1.2.3/b-1.json"},
			wantStored: "gs://records/harbor/tst/v1.2.3/b-1.json",
		},
		{
			name:    "a skipped record says so and writes nothing",
			req:     &RecordRequest{Skipped: "nothing to record"},
			wantOut: []string{"nothing to record"},
		},
		{
			name:    "a failed write is the error",
			req:     &RecordRequest{Bucket: "records", Object: "o.json", Record: record},
			fail:    errors.New("bucket gone"),
			wantErr: "bucket gone",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := &memoryStore{objects: map[string]string{}, fail: tt.fail}
			var out bytes.Buffer
			err := WriteRecord(t.Context(), store.open, tt.req, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("WriteRecord() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("WriteRecord() error = %v", err)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
			if tt.wantStored == "" && len(store.objects) != 0 {
				t.Errorf("stored %v, want nothing", store.objects)
			}
			if tt.wantStored != "" {
				stored, ok := store.objects[tt.wantStored]
				if !ok || !strings.Contains(stored, `"version": "v1.2.3"`) {
					t.Errorf("stored %v, want %s holding the record", store.objects, tt.wantStored)
				}
			}
		})
	}
}
