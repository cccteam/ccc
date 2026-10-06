package derive

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/hook"
	"github.com/cccteam/ccc/impulse/app"
)

func TestHooks(t *testing.T) {
	t.Parallel()

	const header = "package main\n\nimport \"github.com/cccteam/ccc/impulse/deployhook\"\n\n"
	tests := []struct {
		name        string
		program     string
		scripts     []string
		wantScripts []hook.Stage
		wantStages  []hook.Stage
		wantErr     string
	}{
		{name: "scripts alone", scripts: []string{"before-build.sh", "after-down.sh"}, wantScripts: []hook.Stage{hook.AfterDown, hook.BeforeBuild}},
		{
			name:       "a program's stages come from its literal, in the pipeline's order, nil fields left out",
			program:    header + "func main() {\n\tdeployhook.Main(deployhook.Hooks{AfterTraffic: smoke, BeforeMigrate: nil, AfterMigrate: backfill})\n}\n",
			wantStages: []hook.Stage{hook.AfterMigrate, hook.AfterTraffic},
		},
		{
			name:        "a program and scripts mix, one or the other per stage",
			program:     header + "func main() {\n\tdeployhook.Main(deployhook.Hooks{AfterMigrate: backfill})\n}\n",
			scripts:     []string{"before-build.sh"},
			wantScripts: []hook.Stage{hook.BeforeBuild},
			wantStages:  []hook.Stage{hook.AfterMigrate},
		},
		{
			name:    "a stage both implement is refused",
			program: header + "func main() {\n\tdeployhook.Main(deployhook.Hooks{AfterMigrate: backfill})\n}\n",
			scripts: []string{"after-migrate.sh"},
			wantErr: "infrastructure/hooks/after-migrate.sh and cmd/deployment/hooks both implement the after-migrate hook",
		},
		{
			name:    "a program without the literal is refused",
			program: header + "func main() {\n\th := deployhook.Hooks{}\n\th.AfterMigrate = backfill\n\tdeployhook.Main(deployhook.Hooks{})\n}\n",
			wantErr: "holds 2 deployhook.Hooks literals",
		},
		{
			name:    "an unkeyed literal is refused",
			program: header + "func main() {\n\tdeployhook.Main(deployhook.Hooks{nil, backfill, nil, nil})\n}\n",
			wantErr: "must be keyed by field",
		},
		{
			name:    "a program that does not use the contract is refused",
			program: "package main\n\nfunc main() {}\n",
			wantErr: "holds 0 deployhook.Hooks literals",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			write := func(rel, content string) {
				t.Helper()

				abs := filepath.Join(root, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			a := &app.App{Root: root}
			if tt.program != "" {
				write(hooksDir+"/main.go", tt.program)
				write(hooksDir+"/main_test.go", header+"var _ = deployhook.Hooks{BeforeMigrate: nil}\n")
				a.MainPackages = []string{hooksDir}
			}
			for _, s := range tt.scripts {
				write(hook.Dir+"/"+s, "echo\n")
			}
			m := &Model{}
			err := m.hooks(a)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("hooks() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("hooks() error = %v", err)
			}
			if !slices.Equal(m.Hooks, tt.wantScripts) {
				t.Errorf("Hooks = %v, want %v", m.Hooks, tt.wantScripts)
			}
			if (m.HookProgram == nil) != (tt.wantStages == nil) || (m.HookProgram != nil && !slices.Equal(m.HookProgram.Stages, tt.wantStages)) {
				t.Errorf("HookProgram = %+v, want stages %v", m.HookProgram, tt.wantStages)
			}
		})
	}
}
