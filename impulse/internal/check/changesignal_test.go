package check

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

// The engine constructions the change-signal cases build on: one handed the option, one
// without it, one forwarding its options, and one under an aliased import.
const (
	signaledStaff = `package staff

import (
	"context"

	"github.com/cccteam/access"
)

func New(ctx context.Context, signals access.ChangeSignal) error {
	_, err := access.New(nil, access.WithDefaultRoles(nil, nil), access.WithChangeSignal(signals))

	return err
}
`
	signaledMembers = `package members

import (
	"context"

	"github.com/cccteam/access"
)

func New(ctx context.Context, signals access.ChangeSignal) error {
	_, err := access.New(nil, access.WithChangeSignal(signals), access.WithDefaultRoles(nil, nil))

	return err
}
`
	unsignaledStaff = `package staff

import (
	"context"

	"github.com/cccteam/access"
)

func New(ctx context.Context) error {
	_, err := access.New(nil, access.WithDefaultRoles(nil, nil))

	return err
}
`
	forwardingStaff = `package staff

import (
	"context"

	"github.com/cccteam/access"
)

func New(ctx context.Context, opts ...access.Option) error {
	_, err := access.New(nil, opts...)

	return err
}
`
	aliasedStaff = `package staff

import (
	"context"

	acc "github.com/cccteam/access"
)

func New(ctx context.Context, signals acc.ChangeSignal) error {
	_, err := acc.New(nil, acc.WithChangeSignal(signals))

	return err
}
`
	// engineTest constructs an engine in a test, which the check never reads.
	engineTest = `package config

import (
	"testing"

	"github.com/cccteam/access"
)

func TestEngine(t *testing.T) {
	if _, err := access.New(nil); err != nil {
		t.Fatal(err)
	}
}
`
)

func TestChangeSignal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		files       map[string]string
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name:       "every engine handed a change signal",
			files:      map[string]string{"pkg/auth/staff/staff.go": signaledStaff, "pkg/auth/members/members.go": signaledMembers},
			wantStatus: Pass, wantSummary: "2 permission engine(s) constructed, each handed a change signal: pkg/auth/members, pkg/auth/staff",
		},
		{
			name:       "an engine under an aliased import",
			files:      map[string]string{"pkg/auth/staff/staff.go": aliasedStaff},
			wantStatus: Pass, wantSummary: "1 permission engine(s) constructed, each handed a change signal: pkg/auth/staff",
		},
		{
			name:       "an engine handed no change signal fails by package",
			files:      map[string]string{"pkg/auth/staff/staff.go": unsignaledStaff, "pkg/auth/members/members.go": signaledMembers},
			wantStatus: Fail, wantSummary: "1 package(s) construct a permission engine without a change signal",
			wantDetails: []string{"pkg/auth/staff: access.New at pkg/auth/staff/staff.go:10 is handed no change signal; pass access.WithChangeSignal(access.ChangeSignalFunc(announce, watch)) over the application's live service (the policy kind), so a policy write on one instance reaches every instance at once"},
		},
		{
			name:       "an engine forwarding its options cannot be read",
			files:      map[string]string{"pkg/auth/staff/staff.go": forwardingStaff},
			wantStatus: Fail, wantSummary: "1 package(s) construct a permission engine without a change signal",
			wantDetails: []string{"pkg/auth/staff: access.New at pkg/auth/staff/staff.go:10 forwards its options (opts...), so whether a change signal is among them cannot be read here; pass access.WithChangeSignal in this call"},
		},
		{
			name:       "an engine constructed in a test does not count",
			files:      map[string]string{"pkg/auth/staff/staff.go": signaledStaff, "pkg/config/data_test.go": engineTest},
			wantStatus: Pass, wantSummary: "1 permission engine(s) constructed, each handed a change signal: pkg/auth/staff",
		},
		{
			name:       "no engine",
			files:      map[string]string{"pkg/config/doc.go": "package config\n"},
			wantStatus: Skip, wantSummary: "no permission engine is constructed (no access.New outside tests)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			files := map[string]string{"go.mod": "module example.com/harbor\n\ngo 1.26.6\n"}
			for rel, content := range tt.files {
				files[rel] = content
			}
			for rel, content := range files {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			got := changeSignal{}.Run(context.Background(), &Env{App: a})
			if got.Status != tt.wantStatus {
				t.Errorf("Status = %v, want %v (%s)", got.Status, tt.wantStatus, got.Summary)
			}
			if got.Summary != tt.wantSummary {
				t.Errorf("Summary = %q, want %q", got.Summary, tt.wantSummary)
			}
			if diff := cmp.Diff(tt.wantDetails, got.Details); diff != "" {
				t.Errorf("Details mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
