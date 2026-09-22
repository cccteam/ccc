package names

import (
	"strings"
	"testing"
)

func TestApp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		modulePath string
		want       string
	}{
		{name: "the last segment", modulePath: "example.com/acme/beacon", want: "beacon"},
		{name: "before a major version suffix", modulePath: "example.com/acme/beacon/v2", want: "beacon"},
		{name: "lowercased", modulePath: "github.com/CCCTeam/Beacon", want: "beacon"},
		{name: "a hyphenated segment", modulePath: "github.com/cccteam/field-ops", want: "field-ops"},
		{name: "a bare host", modulePath: "example.com", want: "example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := App(tt.modulePath); got != tt.want {
				t.Errorf("App() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateApp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		app     string
		wantErr bool
	}{
		{name: "a word", app: "beacon"},
		{name: "hyphens inside", app: "field-ops"},
		{name: "digits after the first letter", app: "tier2"},
		{name: "the longest allowed", app: strings.Repeat("a", 26)},
		{name: "empty", app: "", wantErr: true},
		{name: "uppercase", app: "Beacon", wantErr: true},
		{name: "a dot", app: "beacon.service", wantErr: true},
		{name: "an underscore", app: "beacon_web", wantErr: true},
		{name: "a leading hyphen", app: "-beacon", wantErr: true},
		{name: "a trailing hyphen", app: "beacon-", wantErr: true},
		{name: "a leading digit", app: "2beacon", wantErr: true},
		{name: "one over the limit", app: strings.Repeat("a", 27), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateApp(tt.app)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateApp() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "application name") {
				t.Errorf("ValidateApp() error = %v, want it to name the application name", err)
			}
		})
	}
}

func TestRenameApp(t *testing.T) {
	t.Parallel()

	const env = "# Development environment for solo. Copy to .envrc and run `direnv allow`.\n" +
		"export APP_SERVICE_NAME=solo\n" +
		"export GOOGLE_CLOUD_SPANNER_PROJECT=solo-dev\n" +
		"export GOOGLE_CLOUD_SPANNER_INSTANCE_ID=solo\n" +
		"export PORT=8090\n" +
		"export SPANNER_EMULATOR_HOST=127.0.0.1:${SPANNER_EMULATOR_PORT}\n" +
		"# solo is the emulator's project too\n"
	const envWant = "# Development environment for beacon. Copy to .envrc and run `direnv allow`.\n" +
		"export APP_SERVICE_NAME=beacon\n" +
		"export GOOGLE_CLOUD_SPANNER_PROJECT=beacon-dev\n" +
		"export GOOGLE_CLOUD_SPANNER_INSTANCE_ID=beacon\n" +
		"export PORT=8090\n" +
		"export SPANNER_EMULATOR_HOST=127.0.0.1:${SPANNER_EMULATOR_PORT}\n" +
		"# solo is the emulator's project too\n"

	tests := []struct {
		name string
		rel  string
		text string
		want string
	}{
		{name: "the workspace name in package.json", rel: "web/package.json", text: "{\n  \"name\": \"solo-web\",\n  \"scripts\": { \"build\": \"ng build console\" }\n}\n", want: "{\n  \"name\": \"beacon-web\",\n  \"scripts\": { \"build\": \"ng build console\" }\n}\n"},
		{name: "a site's workspace name", rel: "apps/console/web/package.json", text: `"name": "solo-console-web"`, want: `"name": "beacon-console-web"`},
		{name: "the workspace name in bun.lock", rel: "web/bun.lock", text: "  \"workspaces\": {\n    \"\": {\n      \"name\": \"solo-web\",\n", want: "  \"workspaces\": {\n    \"\": {\n      \"name\": \"beacon-web\",\n"},
		{name: "another package's name is not the application's", rel: "web/package.json", text: `"name": "solomon-web"`, want: `"name": "solomon-web"`},
		{name: "the environment template's values and heading", rel: ".envrc.template", text: env, want: envWant},
		{name: "an env.example spells the same slots", rel: ".env.example", text: "export APP_SERVICE_NAME=solo\n", want: "export APP_SERVICE_NAME=beacon\n"},
		{name: "the root README heading", rel: "README.md", text: "# solo\n\nAn application. Runs solo.\n", want: "# beacon\n\nAn application. Runs solo.\n"},
		{name: "a README elsewhere", rel: "apps/console/README.md", text: "# solo\n", want: "# solo\n"},
		{name: "a Go file", rel: "main.go", text: "// main serves solo.\n", want: "// main serves solo.\n"},
		{name: "angular.json", rel: "web/angular.json", text: `"name": "solo-web"`, want: `"name": "solo-web"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := RenameApp(tt.rel, tt.text, "solo", "beacon"); got != tt.want {
				t.Errorf("RenameApp() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenameWorkspace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "the workspace", text: `"name": "beacon-web"`, want: `"name": "beacon-console-web"`},
		{name: "any spacing", text: `"name":"beacon-web"`, want: `"name":"beacon-console-web"`},
		{name: "another name", text: `"name": "beacon-web-tools"`, want: `"name": "beacon-web-tools"`},
		{name: "a script that says the name", text: `"build": "echo beacon-web"`, want: `"build": "echo beacon-web"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := RenameWorkspace(tt.text, "beacon-web", "beacon-console-web"); got != tt.want {
				t.Errorf("RenameWorkspace() = %q, want %q", got, tt.want)
			}
		})
	}
}
