package check

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/internal/app"
)

func TestSkipAuth(t *testing.T) {
	t.Parallel()

	oidc := map[string]string{
		"pkg/auth/members/members.go": fmt.Sprintf(membersAuth, "session.DisableRoleSync()"),
		"pkg/config/data.go":          membersConfig,
	}
	with := func(rel, content string) map[string]string {
		files := map[string]string{}
		for k, v := range oidc {
			files[k] = v
		}
		files[rel] = content

		return files
	}

	tests := []struct {
		name        string
		files       map[string]string
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name: "no directory auth", files: map[string]string{"pkg/auth/staff/staff.go": staffAuth, "pkg/config/data.go": staffConfig},
			wantStatus: Skip, wantSummary: "no auth signs in through a directory",
		},
		{
			name: "confined", files: with("Procfile", "server: go run -tags skipAuth .\n"),
			wantStatus: Pass, wantSummary: "1 directory auth(s); the simulated directory is confined to development and tests",
		},
		{
			name: "application code reads the simulated variable", files: with("app/login.go", "package app\n\nimport \"os\"\n\nvar dev = os.Getenv(\"APP_USERNAME\")\n"),
			wantStatus: Fail, wantSummary: "1 simulated-directory problem(s)",
			wantDetails: []string{"app/login.go: reads APP_USERNAME, the simulated directory's variable; only the session library's skipAuth build reads it, and only in development and tests"},
		},
		{
			name: "a test may read it", files: with("test/integration/login_test.go", "package integration\n\nimport \"os\"\n\nvar dev = os.Getenv(\"APP_USERNAME\")\n"),
			wantStatus: Pass, wantSummary: "1 directory auth(s); the simulated directory is confined to development and tests",
		},
		{
			name: "a deployable build carries the tag", files: with("deploy/Dockerfile", "FROM golang\nRUN go build -tags skipAuth -o app .\n"),
			wantStatus: Fail, wantSummary: "1 simulated-directory problem(s)",
			wantDetails: []string{"deploy/Dockerfile: a deployable build carries the skipAuth tag, so the built application would accept any name as a directory login; the tag belongs to the Procfile and the test command only"},
		},
		{
			name: "build files under node_modules are not read", files: with("web/node_modules/left/Dockerfile", "RUN go build -tags skipAuth\n"),
			wantStatus: Pass, wantSummary: "1 directory auth(s); the simulated directory is confined to development and tests",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			write := func(rel, content string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write("go.mod", "module example.com/harbor\n\ngo 1.26.6\n")
			for rel, content := range tt.files {
				write(rel, content)
			}
			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			got := skipAuth{}.Run(context.Background(), &Env{App: a})
			want := Result{Name: "skipauth", Status: tt.wantStatus, Summary: tt.wantSummary, Details: tt.wantDetails}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
