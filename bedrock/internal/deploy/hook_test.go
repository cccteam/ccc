package deploy

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/hook"
)

func TestHook(t *testing.T) {
	t.Parallel()

	const script = "#!/usr/bin/env bash\necho hi\n"
	tests := []struct {
		name    string
		stage   hook.Stage
		env     string
		script  bool
		runErr  error
		wantRan bool
		wantOut string
		wantErr string
	}{
		{name: "a stage that is not one is refused", stage: "before-lunch", wantErr: `"before-lunch" is not a hook stage`},
		{name: "a script runs with the facts in its environment", stage: hook.AfterMigrate, script: true, wantRan: true, wantOut: "=== Hook after-migrate: infrastructure/hooks/after-migrate.sh ==="},
		{name: "no script, nothing runs", stage: hook.BeforeTraffic, wantOut: "No hook at infrastructure/hooks/before-traffic.sh: nothing to run at before-traffic."},
		{name: "a torn-down environment runs nothing", stage: hook.AfterMigrate, env: "export SKIP_DEPLOY=\"true\"\n", script: true, wantOut: tornDown},
		{name: "after-down runs only on a teardown", stage: hook.AfterDown, script: true, wantOut: "Not a teardown: nothing to run after down."},
		{name: "after-down runs on a teardown", stage: hook.AfterDown, env: "export DOWN=\"true\"\nexport SKIP_DEPLOY=\"true\"\n", script: true, wantRan: true},
		{name: "a failing script stops the build", stage: hook.AfterTraffic, script: true, runErr: errors.New("bash failed: exit status 3"), wantRan: true, wantErr: "the after-traffic hook failed (infrastructure/hooks/after-traffic.sh)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := map[string]string{EnvironmentFile: "export RELEASE=\"v1.2.3\"\nexport _ENV='tst'\n" + tt.env}
			if tt.script {
				files[tt.stage.Script()] = script
			}
			w := workspaceFiles(t, files)
			run := &fakeRunner{fail: map[string]error{"bash " + tt.stage.Script(): tt.runErr}}
			var out strings.Builder
			err := Hook(t.Context(), &Clients{Exec: run}, w, tt.stage, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Hook() error = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("Hook() error = %v", err)
			}
			containsAll(t, out.String(), tt.wantOut)
			if got := len(run.ran) == 1; got != tt.wantRan {
				t.Fatalf("ran %v, want a run %t", run.lines(), tt.wantRan)
			}
			if !tt.wantRan {
				return
			}
			c := run.ran[0]
			if c.Dir != string(w) || c.String() != "bash "+tt.stage.Script() {
				t.Errorf("ran %q in %s, want bash on the script in the checkout", c, c.Dir)
			}
			for _, want := range []string{"RELEASE=v1.2.3", "_ENV=tst", "BUILD_ARGS_FILE=" + filepath.Join(string(w), BuildArgsFile)} {
				if !slices.Contains(c.Env, want) {
					t.Errorf("environment %v lacks %s", c.Env, want)
				}
			}
		})
	}
}

func TestOSRunner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		c       Command
		want    string
		wantErr string
	}{
		{name: "a program's output, in its directory with the variables added", c: Command{Dir: "/", Env: []string{"GREETING=hi"}, Name: "sh", Args: []string{"-c", "echo $GREETING from $(pwd)"}}, want: "hi from /\n"},
		{name: "a failing program names itself", c: Command{Name: "sh", Args: []string{"-c", "exit 3"}}, wantErr: "sh failed: exit status 3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var errOut strings.Builder
			got, err := OSRunner{}.Output(t.Context(), tt.c, &errOut)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Output() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil || string(got) != tt.want {
				t.Fatalf("Output() = %q, %v; want %q", got, err, tt.want)
			}
			var out strings.Builder
			if err := (OSRunner{}).Run(t.Context(), tt.c, &out); err != nil || out.String() != tt.want {
				t.Errorf("Run() wrote %q, %v; want %q", out.String(), err, tt.want)
			}
		})
	}
}
