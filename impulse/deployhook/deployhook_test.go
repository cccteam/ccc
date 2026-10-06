package deployhook

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"_APP": "harbor", "_ENV": "tst", "VERSION": "v1.2.3", "RELEASE": "v1.2.3", "IMAGE": "reg/harbor", "IMAGE_DIGEST": "sha256:abc",
		"RUN_MIGRATIONS": "true", "NEXT_URL": "https://harbor-tst-next.example.dev/", "REVISION_URLS": "us-central1=https://next---a.run.app,us-west3=https://next---b.run.app",
		"_SMOKE_PATH": "/healthz",
	}
	tests := []struct {
		name     string
		args     []string
		env      map[string]string
		wantCode int
		wantOut  string
		wantRan  bool
	}{
		{name: "a stage the program implements runs with the facts", args: []string{"after-migrate"}, env: env, wantRan: true},
		{name: "a failing hook exits 1", args: []string{"after-traffic"}, env: env, wantCode: exitFailed, wantOut: "the after-traffic hook failed: the smoke test answered 502"},
		{name: "a stage the program does not implement exits 2", args: []string{"before-traffic"}, env: env, wantCode: exitUsage, wantOut: "it implements [after-migrate after-traffic]"},
		{name: "a stage only a script takes exits 2", args: []string{"before-build"}, env: env, wantCode: exitUsage},
		{name: "no stage exits 2", env: env, wantCode: exitUsage, wantOut: "usage: hooks <stage>"},
		{name: "a pull request number that is not one exits 2", args: []string{"after-migrate"}, env: map[string]string{"_PR_NUMBER": "seven"}, wantCode: exitUsage, wantOut: `_PR_NUMBER "seven"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var seen *Facts
			hooks := Hooks{
				AfterMigrate: func(_ context.Context, f *Facts) error {
					seen = f

					return nil
				},
				AfterTraffic: func(context.Context, *Facts) error {
					return errors.New("the smoke test answered 502")
				},
			}
			var out strings.Builder
			code := run(t.Context(), hooks, tt.args, func(name string) string {
				return tt.env[name]
			}, &out)
			if code != tt.wantCode {
				t.Fatalf("run() = %d, want %d; output %q", code, tt.wantCode, out.String())
			}
			if !strings.Contains(out.String(), tt.wantOut) {
				t.Errorf("output %q lacks %q", out.String(), tt.wantOut)
			}
			if (seen != nil) != tt.wantRan {
				t.Fatalf("the hook ran %t, want %t", seen != nil, tt.wantRan)
			}
			if !tt.wantRan {
				return
			}
			if seen.App != "harbor" || seen.Environment != "tst" || seen.ImageDigest != "sha256:abc" || !seen.RunMigrations || seen.PullRequest != 0 {
				t.Errorf("facts = %+v", seen)
			}
			if seen.RevisionURLs["us-west3"] != "https://next---b.run.app" || seen.Substitution("_SMOKE_PATH") != "/healthz" {
				t.Errorf("revision URLs %v, _SMOKE_PATH %q", seen.RevisionURLs, seen.Substitution("_SMOKE_PATH"))
			}
		})
	}
}

func TestFactsFrom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		env    map[string]string
		wantPR int
		shared bool
	}{
		{name: "a release build", env: map[string]string{"_ENV": "stg"}},
		{name: "a pull request in shared mode", env: map[string]string{"_PR_NUMBER": "7", "SHARED_DB": "true"}, wantPR: 7, shared: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, err := FactsFrom(func(name string) string {
				return tt.env[name]
			})
			if err != nil {
				t.Fatalf("FactsFrom() error = %v", err)
			}
			if f.PullRequest != tt.wantPR || f.SharedDB != tt.shared {
				t.Errorf("facts = %+v", f)
			}
		})
	}
}
