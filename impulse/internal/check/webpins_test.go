package check

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/mod/modfile"

	"github.com/cccteam/ccc/impulse/app"
)

// webStep answers the checks' questions about the ledger with fixed web pins: at the last
// step, and the step's web pins as given.
type webStep map[string]string

func (webStep) AtLastStep(*modfile.File) bool { return true }

func (l webStep) WebPins(*modfile.File) map[string]string { return l }

// TestWebPins holds every web root to the step's web pins: a root at them passes; one
// behind in any section fails naming the file, the section and the package; one ahead
// warns; one naming a package nowhere fails; a root naming none of the library's packages
// is passed over; and the check skips without browser apps, without a ledger, or at a step
// naming no web pins.
func TestWebPins(t *testing.T) {
	t.Parallel()

	step := webStep{"@cccteam/resource": "0.0.12", "@cccteam/resource-angular": "0.0.16", "@cccteam/resource-firestore": "0.0.10"}
	at := `{"dependencies": {"@angular/core": "^21.1.1", "@cccteam/resource": "0.0.12", "@cccteam/resource-angular": "0.0.16", "@cccteam/resource-firestore": "0.0.10"}, "overrides": {"@cccteam/resource": "0.0.12"}}`
	tests := []struct {
		name string
		// roots are the web roots' package.json files by web directory.
		roots       map[string]string
		ledger      Ledger
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name: "every web root at the step's pins", roots: map[string]string{"web": at, "apps/portal/web": at}, ledger: step,
			wantStatus: Pass, wantSummary: "2 web root(s) pin the web library at the ledger step's versions (@cccteam/resource 0.0.12, @cccteam/resource-angular 0.0.16, @cccteam/resource-firestore 0.0.10)",
		},
		{
			name:       "a root behind the step in the dependencies and the overrides",
			roots:      map[string]string{"web": `{"dependencies": {"@cccteam/resource": "0.0.11", "@cccteam/resource-angular": "0.0.15", "@cccteam/resource-firestore": "0.0.10"}, "overrides": {"@cccteam/resource": "0.0.11"}}`},
			ledger:     step,
			wantStatus: Fail, wantSummary: "3 web pin(s) differ from the ledger step the application stands at (@cccteam/resource 0.0.12, @cccteam/resource-angular 0.0.16, @cccteam/resource-firestore 0.0.10); impulse upgrade moves them",
			wantDetails: []string{
				"web/package.json dependencies: @cccteam/resource is 0.0.11 and the step pins 0.0.12",
				"web/package.json dependencies: @cccteam/resource-angular is 0.0.15 and the step pins 0.0.16",
				"web/package.json overrides: @cccteam/resource is 0.0.11 and the step pins 0.0.12",
			},
		},
		{
			name:       "a range and a yalc attachment are behind",
			roots:      map[string]string{"web": `{"dependencies": {"@cccteam/resource": "^0.0.12", "@cccteam/resource-angular": "file:.yalc/@cccteam/resource-angular", "@cccteam/resource-firestore": "0.0.10"}}`},
			ledger:     step,
			wantStatus: Fail, wantSummary: "2 web pin(s) differ from the ledger step the application stands at (@cccteam/resource 0.0.12, @cccteam/resource-angular 0.0.16, @cccteam/resource-firestore 0.0.10); impulse upgrade moves them",
			wantDetails: []string{
				"web/package.json dependencies: @cccteam/resource is ^0.0.12 and the step pins 0.0.12",
				"web/package.json dependencies: @cccteam/resource-angular is file:.yalc/@cccteam/resource-angular and the step pins 0.0.16",
			},
		},
		{
			name:       "a root ahead of the step warns",
			roots:      map[string]string{"web": `{"dependencies": {"@cccteam/resource": "0.0.12", "@cccteam/resource-angular": "0.0.17", "@cccteam/resource-firestore": "0.0.10"}}`},
			ledger:     step,
			wantStatus: Warn, wantSummary: "1 web pin(s) stand ahead of the ledger step the application stands at (@cccteam/resource 0.0.12, @cccteam/resource-angular 0.0.16, @cccteam/resource-firestore 0.0.10)",
			wantDetails: []string{"web/package.json dependencies: @cccteam/resource-angular is 0.0.17, ahead of the step's 0.0.16; a pin ahead of its step stays until a step reaches it"},
		},
		{
			name:       "a root naming a package nowhere fails; one behind beside one ahead fails with the warning listed after",
			roots:      map[string]string{"web": `{"dependencies": {"@cccteam/resource": "0.0.11", "@cccteam/resource-angular": "0.0.17"}}`},
			ledger:     step,
			wantStatus: Fail, wantSummary: "2 web pin(s) differ from the ledger step the application stands at (@cccteam/resource 0.0.12, @cccteam/resource-angular 0.0.16, @cccteam/resource-firestore 0.0.10); impulse upgrade moves them",
			wantDetails: []string{
				"web/package.json dependencies: @cccteam/resource is 0.0.11 and the step pins 0.0.12",
				"web/package.json names no @cccteam/resource-firestore, which the step pins at 0.0.10; add it to the dependencies as the skeleton's web root has it",
				"web/package.json dependencies: @cccteam/resource-angular is 0.0.17, ahead of the step's 0.0.16; a pin ahead of its step stays until a step reaches it",
			},
		},
		{
			name: "a root naming none of the library's packages is passed over", roots: map[string]string{"web": at, "site/web": `{"dependencies": {"@angular/core": "^21.1.1"}}`}, ledger: step,
			wantStatus: Pass, wantSummary: "1 web root(s) pin the web library at the ledger step's versions (@cccteam/resource 0.0.12, @cccteam/resource-angular 0.0.16, @cccteam/resource-firestore 0.0.10)",
		},
		{
			name: "no root names the library", roots: map[string]string{"web": `{"dependencies": {"@angular/core": "^21.1.1"}}`}, ledger: step,
			wantStatus: Skip, wantSummary: "no web root names the web library",
		},
		{name: "no browser apps", ledger: step, wantStatus: Skip, wantSummary: "no browser apps"},
		{name: "no ledger", roots: map[string]string{"web": at}, wantStatus: Skip, wantSummary: "no ledger to read the step from"},
		{name: "a step naming no web pins", roots: map[string]string{"web": at}, ledger: atStep(true), wantStatus: Skip, wantSummary: "the ledger step the application stands at names no web pins"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			a := &app.App{Root: dir}
			for _, web := range sortedKeys(tt.roots) {
				a.WebApps = append(a.WebApps, app.WebApp{Dir: web})
				if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(web)), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(web), "package.json"), []byte(tt.roots[web]), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got := webPins{}.Run(context.Background(), &Env{App: a, Ledger: tt.ledger})
			if got.Status != tt.wantStatus || got.Summary != tt.wantSummary {
				t.Fatalf("Run() = %s %q, want %s %q (details %v)", got.Status, got.Summary, tt.wantStatus, tt.wantSummary, got.Details)
			}
			if diff := cmp.Diff(strings.Join(tt.wantDetails, "\n"), strings.Join(got.Details, "\n")); diff != "" {
				t.Errorf("Details mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestWebVersion reads a spec as a version: an exact release and nothing else.
func TestWebVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		spec       string
		want       string
		wantBehind bool
	}{
		{spec: "0.0.16", want: "v0.0.16", wantBehind: false},
		{spec: "0.0.17", want: "v0.0.17", wantBehind: false},
		{spec: "0.0.15", want: "v0.0.15", wantBehind: true},
		{spec: "1.2.3", want: "v1.2.3", wantBehind: false},
		{spec: "^0.0.16", want: "", wantBehind: true},
		{spec: "~0.0.16", want: "", wantBehind: true},
		{spec: "v0.0.16", want: "", wantBehind: true},
		{spec: "0.0.16-rc.1", want: "", wantBehind: true},
		{spec: "0.0.16+build", want: "", wantBehind: true},
		{spec: "latest", want: "", wantBehind: true},
		{spec: "file:.yalc/@cccteam/resource", want: "", wantBehind: true},
		{spec: "", want: "", wantBehind: true},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			t.Parallel()

			if got := WebVersion(tt.spec); got != tt.want {
				t.Errorf("WebVersion(%q) = %q, want %q", tt.spec, got, tt.want)
			}
			if got := WebBehind(tt.spec, "0.0.16"); got != tt.wantBehind {
				t.Errorf("WebBehind(%q, 0.0.16) = %v, want %v", tt.spec, got, tt.wantBehind)
			}
		})
	}
}

// TestWebRoots reads the web roots naming the library: each root's pins section by
// section, a root naming none left out, a root without a package.json left out.
func TestWebRoots(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files := map[string]string{
		"web/package.json":       `{"dependencies": {"@cccteam/resource-angular": "0.0.16", "@cccteam/resource": "0.0.12"}, "peerDependencies": {"@cccteam/resource": "0.0.12"}, "overrides": {"@cccteam/resource": "0.0.12", "nested": {"x": "1"}}}`,
		"plain/web/package.json": `{"dependencies": {"@angular/core": "^21.1.1"}}`,
	}
	for rel, content := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, filepath.FromSlash(rel))), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a := &app.App{Root: dir, WebApps: []app.WebApp{{Dir: "web"}, {Dir: "plain/web"}, {Dir: "missing/web"}}}
	got, err := WebRoots(a, []string{"@cccteam/resource", "@cccteam/resource-angular", "@cccteam/resource-firestore"})
	if err != nil {
		t.Fatal(err)
	}
	want := []WebRoot{{Dir: "web", File: "web/package.json", Pins: []WebPin{
		{Section: "dependencies", Package: "@cccteam/resource", Version: "0.0.12"},
		{Section: "dependencies", Package: "@cccteam/resource-angular", Version: "0.0.16"},
		{Section: "peerDependencies", Package: "@cccteam/resource", Version: "0.0.12"},
		{Section: "overrides", Package: "@cccteam/resource", Version: "0.0.12"},
	}}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("WebRoots() mismatch (-want +got):\n%s", diff)
	}
	if spec := got[0].Spec("@cccteam/resource-firestore"); spec != "" {
		t.Errorf("Spec(firestore) = %q, want none", spec)
	}
	if spec := got[0].Spec("@cccteam/resource"); spec != "0.0.12" {
		t.Errorf("Spec(resource) = %q, want 0.0.12", spec)
	}
	if line := WebLine(map[string]string{"@cccteam/resource-angular": "0.0.16", "@cccteam/resource": "0.0.12"}); line != "@cccteam/resource 0.0.12, @cccteam/resource-angular 0.0.16" {
		t.Errorf("WebLine() = %q", line)
	}
}
