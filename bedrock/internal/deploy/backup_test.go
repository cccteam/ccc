package deploy

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestStartReleaseBackup(t *testing.T) {
	t.Parallel()

	const (
		databases = `export MIGRATE_DATABASES='["projects/spn/instances/imp-spn-gbl-spanner/databases/imp-prd-gbl-harbor-db","projects/prd-project/databases/imp-prd-gbl-harbor-fs"]'` + "\n"
		keeping   = "export SKIP_DEPLOY=\"\"\nexport RELEASE=\"v1.2.3\"\nexport RUN_MIGRATIONS=\"true\"\nexport KEEPS_RELEASE_BACKUPS=\"true\"\nexport RESTORE=\"\"\nexport ROLLBACK=\"\"\n" + databases
	)
	now := time.Date(2026, 10, 5, 4, 30, 15, 500, time.UTC)
	subs := map[string]string{"_PROJECT": "prd-project", "_ENV": "prd", "_APP": "harbor", "_APPLY_IDENTITY": "imp-prd-gbl-harbor-tofu@prd-project.iam.gserviceaccount.com", "TAG_NAME": "v1.2.3"}
	tests := []struct {
		name   string
		env    string
		subs   map[string]string
		refuse string
		// pending is how many starts Spanner refuses first because it is taking another
		// backup of the database.
		pending int
		wantOut []string
		// wantCreated is the backup the step asked Spanner for, as the fake records it;
		// wantFacts the facts it leaves.
		wantCreated []string
		wantFacts   map[string]string
		wantErr     string
	}{
		{
			name: "a release in an environment keeping release backups starts the backup as of the cut and leaves the facts",
			env:  keeping,
			subs: subs,
			wantOut: []string{
				"=== Release backup: imp-prd-gbl-harbor-db-pre-v1-2-3-b-1 holds imp-prd-gbl-harbor-db as of the cut, 2026-10-05T04:30:15Z, and is kept until 2026-10-19T04:30:15Z (operation projects/spn/instances/imp-spn-gbl-spanner/operations/op-imp-prd-gbl-harbor-db-pre-v1-2-3-b-1) ===",
				"Spanner takes the backup in the background while v1.2.3 goes on; the migrations that follow change the live database alone. A release gone wrong is rolled back to it with bedrock rollback prd.",
			},
			wantCreated: []string{"imp-prd-gbl-harbor-db-pre-v1-2-3-b-1 of imp-prd-gbl-harbor-db as of 2026-10-05T04:30:15Z until 2026-10-19T04:30:15Z"},
			wantFacts: map[string]string{
				"CUT": "2026-10-05T04:30:15Z", "RELEASE_BACKUP": "projects/spn/instances/imp-spn-gbl-spanner/backups/imp-prd-gbl-harbor-db-pre-v1-2-3-b-1",
				"RELEASE_BACKUP_TIME": "2026-10-05T04:30:15Z", "RELEASE_BACKUP_EXPIRES": "2026-10-19T04:30:15Z",
			},
		},
		{
			name:        "a later generation of the database names the backup after it",
			env:         strings.Replace(keeping, "imp-prd-gbl-harbor-db\"", "imp-prd-gbl-harbor-db-2\"", 1),
			subs:        subs,
			wantCreated: []string{"imp-prd-gbl-harbor-db-2-pre-v1-2-3-b-1 of imp-prd-gbl-harbor-db-2 as of 2026-10-05T04:30:15Z until 2026-10-19T04:30:15Z"},
			wantFacts:   map[string]string{"RELEASE_BACKUP": "projects/spn/instances/imp-spn-gbl-spanner/backups/imp-prd-gbl-harbor-db-2-pre-v1-2-3-b-1"},
		},
		{
			name:    "an environment off the releaseBackups list takes none",
			env:     strings.Replace(keeping, "KEEPS_RELEASE_BACKUPS=\"true\"", "KEEPS_RELEASE_BACKUPS=\"false\"", 1),
			subs:    subs,
			wantOut: []string{"No release backup: the environment is not on the placement's releaseBackups list, so no backup is taken as of the cut and bedrock rollback does not serve it."},
		},
		{
			name:    "a run that applies no migration takes none",
			env:     strings.Replace(keeping, "RUN_MIGRATIONS=\"true\"", "RUN_MIGRATIONS=\"false\"", 1),
			subs:    subs,
			wantOut: []string{"No release backup: this run applies no migration, so the database stays as the live release left it."},
		},
		{
			name:    "a restore run takes none",
			env:     strings.Replace(keeping, "RESTORE=\"\"", "RESTORE=\"production-backup\"", 1),
			subs:    subs,
			wantOut: []string{"No release backup: a restore run replaces the database (production-backup), so there is no state before the migrations to keep."},
		},
		{
			name:    "a rollback run takes none",
			env:     strings.Replace(keeping, "ROLLBACK=\"\"", "ROLLBACK=\"projects/spn/instances/imp-spn-gbl-spanner/backups/imp-prd-gbl-harbor-db-pre-v1-2-2\"", 1),
			subs:    subs,
			wantOut: []string{"No release backup: a rollback restores a backup into the database's next generation; the state before it is that backup, and the live database stays as the forensic copy."},
		},
		{
			name:    "a run that deploys nothing takes none",
			env:     strings.Replace(keeping, "SKIP_DEPLOY=\"\"", "SKIP_DEPLOY=\"true\"\nexport SKIP_REASON=\"version: nothing deploys\"", 1),
			subs:    subs,
			wantOut: []string{"No release backup: nothing deploys in this run (version: nothing deploys)."},
		},
		{
			name:    "a pull-request build takes none",
			env:     keeping,
			subs:    map[string]string{"_PROJECT": "tst-project", "_ENV": "tst", "_APP": "harbor", "_PR_NUMBER": "7"},
			wantOut: []string{pullRequestBuildNotice},
		},
		{
			name:    "a release backup waits while Spanner takes another backup of the database",
			env:     keeping,
			subs:    subs,
			pending: 3,
			wantOut: []string{
				"Waiting to start the release backup imp-prd-gbl-harbor-db-pre-v1-2-3-b-1: Spanner is taking another backup of imp-prd-gbl-harbor-db, and takes one at a time (0s so far); it starts when that one completes.",
				"=== Release backup: imp-prd-gbl-harbor-db-pre-v1-2-3-b-1 holds imp-prd-gbl-harbor-db as of the cut, 2026-10-05T04:30:15Z",
			},
			wantCreated: []string{"imp-prd-gbl-harbor-db-pre-v1-2-3-b-1 of imp-prd-gbl-harbor-db as of 2026-10-05T04:30:15Z until 2026-10-19T04:30:15Z"},
			wantFacts:   map[string]string{"RELEASE_BACKUP": "projects/spn/instances/imp-spn-gbl-spanner/backups/imp-prd-gbl-harbor-db-pre-v1-2-3-b-1"},
		},
		{
			name:    "a backup Spanner refuses stops the run before the migrations",
			env:     keeping,
			subs:    subs,
			refuse:  "the backup quota is reached",
			wantErr: "starting the release backup imp-prd-gbl-harbor-db-pre-v1-2-3-b-1 of imp-prd-gbl-harbor-db as of 2026-10-05T04:30:15Z",
		},
		{
			name:    "a run without the stack's databases is refused",
			env:     strings.Replace(keeping, databases, "", 1),
			subs:    subs,
			wantErr: "environment.sh names no databases for the migrate command (MIGRATE_DATABASES)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := workspaceFiles(t, map[string]string{EnvironmentFile: tt.env, BuildFile: buildFor(t, tt.subs)})
			store := &fakeSpanner{refuse: tt.refuse, pending: tt.pending}
			clients := &Clients{SpannerAs: store.open, Sleep: noSleep}
			var out strings.Builder
			err := StartReleaseBackup(t.Context(), clients, w, now, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("StartReleaseBackup() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("StartReleaseBackup() error = %v; output:\n%s", err, out.String())
			}
			containsAll(t, out.String(), tt.wantOut...)
			if diff := cmp.Diff(tt.wantCreated, store.created); diff != "" {
				t.Errorf("backups created (-want +got):\n%s", diff)
			}
			env, err := w.Environment()
			if err != nil {
				t.Fatalf("Environment() error = %v", err)
			}
			for name, want := range tt.wantFacts {
				if env[name] != want {
					t.Errorf("%s = %q, want %q", name, env[name], want)
				}
			}
			if len(tt.wantFacts) == 0 && env[releaseBackupFact] != "" {
				t.Errorf("RELEASE_BACKUP = %q, want none", env[releaseBackupFact])
			}
		})
	}
}
