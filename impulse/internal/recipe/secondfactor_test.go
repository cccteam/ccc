package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

// staffRuled is an auth package whose New sets the sign-in rule spliced into the first
// slot, the second slot holding the package's rule functions; the construction is on
// line 21, the policy statement on line 26.
const staffRuled = `package staff

import (
	"context"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessioninfo"
	"github.com/cccteam/session/sessionstorage"
	"github.com/cccteam/session/signin"
)

type Session = session.Auth[session.NoCustomData, session.NoCustomData, *cloudspanner.ReadWriteTransaction]

type Auth struct {
	session *Session
}

func New(ctx context.Context, db *cloudspanner.Client, key string) (*Auth, error) {
	_ = ctx
	s, err := session.NewAuth[session.NoCustomData, session.NoCustomData](sessionstorage.NewSpannerAuth(db), key)
	if err != nil {
		return nil, err
	}
	session.PasswordSignIn(s)
%s
	return &Auth{session: s}, nil
}

var _ = sessioninfo.MethodEmailCode

var _ = signin.Allow
%s`

const ruleFunc = `
func requireSecondFactor(context.Context, *cloudspanner.ReadWriteTransaction, *sessioninfo.NewSessionRequest) (*signin.Decision, error) {
	return signin.RequireMFA(sessioninfo.MethodEmailCode), nil
}
`

func TestSecondFactor(t *testing.T) {
	t.Parallel()

	const byHand = "impulse upgrade's second-factor recipe or impulse check --fix writes it; the skeleton's auth package is the pattern"
	tests := []struct {
		name       string
		policy     string
		funcs      string
		wantDetect []string
		wantDid    []string
		wantLeft   []string
		wantSource []string
	}{
		{
			name:       "an auth without a rule gains the call and the function",
			wantDetect: []string{"pkg/auth/staff/staff.go:21: staff.New sets no sign-in rule on the Auth it constructs, so the password alone admits a session; every session carries a second factor: set s.SetSignInPolicy(requireSecondFactor), requireSecondFactor returning signin.RequireMFA(sessioninfo.MethodEmailCode) (" + byHand + ")"},
			wantDid: []string{
				"pkg/auth/staff/staff.go:21: s.SetSignInPolicy(requireSecondFactor) set after the staff auth's construction, so every sign-in waits for the second factor",
				"pkg/auth/staff/staff.go: requireSecondFactor written, the rule returning signin.RequireMFA(sessioninfo.MethodEmailCode), with its imports",
			},
			wantLeft:   []string{"pkg/auth/staff: the tests that sign in to the staff auth with a password now meet the code step before any session opens; complete it as the skeleton's integration tests do (test/integration/second_factor_test.go: the code read from the mail.Recorder the auth is constructed to send to, entered at user/pending/code/verify)"},
			wantSource: []string{"\t}\n\n\t// The sign-in rule: every session carries a second factor (requireSecondFactor).\n\ts.SetSignInPolicy(requireSecondFactor)\n", "func requireSecondFactor(context.Context, *cloudspanner.ReadWriteTransaction, *sessioninfo.NewSessionRequest) (*signin.Decision, error) {"},
		},
		{
			name:       "an auth whose rule ends with the tenant policy gains the chain",
			policy:     "\ts.SetSignInPolicy(signin.TenantPolicy(nil, nil))",
			wantDetect: []string{"pkg/auth/staff/staff.go:26: the staff auth's sign-in rule ends with signin.TenantPolicy(nil, nil), which is no function of the package; chain the second factor last: signin.TenantPolicy(nil, nil).Then(requireSecondFactor) (" + byHand + ")"},
			wantDid: []string{
				"pkg/auth/staff/staff.go:26: .Then(requireSecondFactor) chained on the staff auth's sign-in rule, so a sign-in the rule admits waits for the second factor",
				"pkg/auth/staff/staff.go: requireSecondFactor written, the rule returning signin.RequireMFA(sessioninfo.MethodEmailCode), with its imports",
			},
			wantLeft:   []string{"pkg/auth/staff: the tests that sign in to the staff auth with a password now meet the code step before any session opens; complete it as the skeleton's integration tests do (test/integration/second_factor_test.go: the code read from the mail.Recorder the auth is constructed to send to, entered at user/pending/code/verify)"},
			wantSource: []string{"\ts.SetSignInPolicy(signin.TenantPolicy(nil, nil).Then(requireSecondFactor))\n"},
		},
		{
			name:   "an auth whose rule ends with the second factor has nothing to do",
			policy: "\ts.SetSignInPolicy(requireSecondFactor)",
			funcs:  ruleFunc,
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
			write("pkg/auth/staff/staff.go", fmt.Sprintf(staffRuled, tt.policy, tt.funcs))
			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			r := SecondFactor{}
			found, err := r.Detect(t.Context(), a)
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantDetect, found); diff != "" {
				t.Errorf("Detect() mismatch (-want +got):\n%s", diff)
			}
			ch, err := r.Apply(t.Context(), a, nil)
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantDid, ch.Did); diff != "" {
				t.Errorf("Apply() Did mismatch (-want +got):\n%s", diff)
			}
			if len(ch.Skipped) != 0 {
				t.Errorf("Apply() skipped %v", ch.Skipped)
			}
			if diff := cmp.Diff(tt.wantLeft, ch.Left); diff != "" {
				t.Errorf("Apply() Left mismatch (-want +got):\n%s", diff)
			}
			wantNote := ""
			if len(tt.wantLeft) > 0 {
				wantNote = "Every password sign-in now waits for the mailed code, so a test that signs in and expects a session completes the code step first; the skeleton's integration tests are the pattern."
			}
			if ch.LeftNote != wantNote {
				t.Errorf("Apply() LeftNote = %q, want %q", ch.LeftNote, wantNote)
			}
			src, err := os.ReadFile(filepath.Join(root, "pkg", "auth", "staff", "staff.go"))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.wantSource {
				if !strings.Contains(string(src), want) {
					t.Errorf("the package lacks %q:\n%s", want, src)
				}
			}
			// The recipe is safe to run twice: the edited package has nothing left to do.
			again, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() after the edit error = %v", err)
			}
			if found, err := r.Detect(t.Context(), again); err != nil || len(found) != 0 {
				t.Errorf("Detect() after the edit = %v, %v; want nothing", found, err)
			}
		})
	}
}
