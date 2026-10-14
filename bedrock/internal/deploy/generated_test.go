package deploy

import (
	"maps"
	"slices"
	"testing"
)

// TestGeneratedArgs: the plan's variable for the derived versions, sorted by variable as
// tofu takes a map on the command line, and nothing without any; the fact the record
// reads back, and the read.
func TestGeneratedArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		pins     map[string]string
		wantArgs []string
		wantFact string
	}{
		{name: "no generated secret", pins: nil},
		{name: "one", pins: map[string]string{"APP_COOKIE_KEY": "3"}, wantArgs: []string{"-var", `generated_secret_versions={APP_COOKIE_KEY="3"}`}, wantFact: "APP_COOKIE_KEY=3"},
		{name: "several, sorted", pins: map[string]string{"APP_SIGNING_KEY": "12", "APP_COOKIE_KEY": "3"}, wantArgs: []string{"-var", `generated_secret_versions={APP_COOKIE_KEY="3", APP_SIGNING_KEY="12"}`}, wantFact: "APP_COOKIE_KEY=3,APP_SIGNING_KEY=12"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := generatedArgs(tt.pins); !slices.Equal(got, tt.wantArgs) {
				t.Errorf("generatedArgs() = %q, want %q", got, tt.wantArgs)
			}
			fact := generatedFactValue(tt.pins)
			if fact != tt.wantFact {
				t.Errorf("generatedFactValue() = %q, want %q", fact, tt.wantFact)
			}
			got := generatedOf(map[string]string{generatedFact: fact})
			if len(tt.pins) == 0 && got != nil {
				t.Errorf("generatedOf() = %v, want nil", got)
			}
			if len(tt.pins) > 0 && !maps.Equal(got, tt.pins) {
				t.Errorf("generatedOf() = %v, want %v", got, tt.pins)
			}
		})
	}
}

// TestIdentityProject: the project a service account's email belongs to.
func TestIdentityProject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		email string
		want  string
	}{
		{email: "quill-plan@p-stg.iam.gserviceaccount.com", want: "p-stg"},
		{email: "imp-tst-gbl-harbor-apply@imp-tst-gbl-core-b241.iam.gserviceaccount.com", want: "imp-tst-gbl-core-b241"},
		{email: "nobody", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			t.Parallel()

			if got := identityProject(tt.email); got != tt.want {
				t.Errorf("identityProject(%q) = %q, want %q", tt.email, got, tt.want)
			}
		})
	}
}
