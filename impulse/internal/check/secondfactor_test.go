package check

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

// ruledAuth is an auth package whose New sets the sign-in rule spliced into the first
// slot, the second slot holding the package's rule functions. The construction is on line
// 23, the policy statement on line 28.
const ruledAuth = `package staff

import (
	"context"
	_ "embed"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessioninfo"
	"github.com/cccteam/session/sessionstorage"
	"github.com/cccteam/session/signin"
)

type Session = session.Auth[session.NoCustomData, session.NoCustomData, *cloudspanner.ReadWriteTransaction]

type Auth struct {
	session *Session
}

func New(ctx context.Context, db *cloudspanner.Client, key string, collection access.PermissionCollection) (*Auth, error) {
	_ = access.PermissionCollection(collection)
	s, err := session.NewAuth[session.NoCustomData, session.NoCustomData](sessionstorage.NewSpannerAuth(db), key)
	if err != nil {
		return nil, err
	}
	session.PasswordSignIn(s)
%s
	return &Auth{session: s}, nil
}

func (a *Auth) Session() *Session { return a.session }

var _ = sessioninfo.MethodEmailCode

var _ = signin.Allow
%s`

const (
	secondFactorFunc = `
func requireSecondFactor(context.Context, *cloudspanner.ReadWriteTransaction, *sessioninfo.NewSessionRequest) (*signin.Decision, error) {
	return signin.RequireMFA(sessioninfo.MethodEmailCode), nil
}
`
	allowFunc = `
func allowEveryone(context.Context, *cloudspanner.ReadWriteTransaction, *sessioninfo.NewSessionRequest) (*signin.Decision, error) {
	return signin.Allow(), nil
}
`
)

func TestSecondFactor(t *testing.T) {
	t.Parallel()

	const byHand = "impulse upgrade's second-factor recipe or impulse check --fix writes it; the skeleton's auth package is the pattern"
	tests := []struct {
		name        string
		files       map[string]string
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name:       "the rule alone",
			files:      map[string]string{"pkg/auth/staff/staff.go": fmt.Sprintf(ruledAuth, "\ts.SetSignInPolicy(requireSecondFactor)", secondFactorFunc)},
			wantStatus: Pass, wantSummary: "1 auth(s) end their sign-in rule with the second factor: staff",
		},
		{
			name:       "the rule chained last after the tenant policy",
			files:      map[string]string{"pkg/auth/staff/staff.go": fmt.Sprintf(ruledAuth, "\ts.SetSignInPolicy(signin.TenantPolicy(nil, nil).Then(requireSecondFactor))", secondFactorFunc)},
			wantStatus: Pass, wantSummary: "1 auth(s) end their sign-in rule with the second factor: staff",
		},
		{
			name:       "no rule",
			files:      map[string]string{"pkg/auth/staff/staff.go": fmt.Sprintf(ruledAuth, "", "")},
			wantStatus: Fail, wantSummary: "1 auth(s) without the second factor",
			wantDetails: []string{"pkg/auth/staff/staff.go:23: staff.New sets no sign-in rule on the Auth it constructs, so the password alone admits a session; every session carries a second factor: set s.SetSignInPolicy(requireSecondFactor), requireSecondFactor returning signin.RequireMFA(sessioninfo.MethodEmailCode) (" + byHand + ")"},
		},
		{
			name:       "the rule ends with the tenant policy",
			files:      map[string]string{"pkg/auth/staff/staff.go": fmt.Sprintf(ruledAuth, "\ts.SetSignInPolicy(signin.TenantPolicy(nil, nil))", "")},
			wantStatus: Fail, wantSummary: "1 auth(s) without the second factor",
			wantDetails: []string{"pkg/auth/staff/staff.go:28: the staff auth's sign-in rule ends with signin.TenantPolicy(nil, nil), which is no function of the package; chain the second factor last: signin.TenantPolicy(nil, nil).Then(requireSecondFactor) (" + byHand + ")"},
		},
		{
			name:       "the rule ends with a function that requires no second factor",
			files:      map[string]string{"pkg/auth/staff/staff.go": fmt.Sprintf(ruledAuth, "\ts.SetSignInPolicy(signin.TenantPolicy(nil, nil).Then(allowEveryone))", allowFunc)},
			wantStatus: Fail, wantSummary: "1 auth(s) without the second factor",
			wantDetails: []string{"pkg/auth/staff/staff.go:28: the staff auth's sign-in rule ends with allowEveryone, which returns no signin.RequireMFA decision, so no sign-in waits for the code; chain the second factor last: allowEveryone.Then(requireSecondFactor), requireSecondFactor returning signin.RequireMFA(sessioninfo.MethodEmailCode) (" + byHand + ")"},
		},
		{
			name:       "a second policy call",
			files:      map[string]string{"pkg/auth/staff/staff.go": fmt.Sprintf(ruledAuth, "\ts.SetSignInPolicy(signin.TenantPolicy(nil, nil))\n\ts.SetSignInPolicy(requireSecondFactor)", secondFactorFunc)},
			wantStatus: Fail, wantSummary: "1 auth(s) without the second factor",
			wantDetails: []string{
				"pkg/auth/staff/staff.go:28: the staff auth's sign-in rule ends with signin.TenantPolicy(nil, nil), which is no function of the package; chain the second factor last: signin.TenantPolicy(nil, nil).Then(requireSecondFactor) (" + byHand + ")",
				"pkg/auth/staff/staff.go:29: a second SetSignInPolicy call on the staff auth; the session library panics at a second policy: chain it on the first with .Then",
			},
		},
		{
			name:       "no auth package",
			files:      map[string]string{"pkg/config/data.go": "package config\n"},
			wantStatus: Skip, wantSummary: "no auth package",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := discoverFiles(t, tt.files)
			got := secondFactor{}.Run(context.Background(), &Env{App: a})
			want := Result{Name: "second-factor", Status: tt.wantStatus, Summary: tt.wantSummary, Details: tt.wantDetails}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSecondFactorFix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		policy      string
		funcs       string
		wantDetails []string
		wantSource  []string
	}{
		{
			name: "no rule: the call is set after the construction and the function written",
			wantDetails: []string{
				"pkg/auth/staff/staff.go:23: s.SetSignInPolicy(requireSecondFactor) set after the staff auth's construction, so every sign-in waits for the second factor",
				"pkg/auth/staff/staff.go: requireSecondFactor written, the rule returning signin.RequireMFA(sessioninfo.MethodEmailCode), with its imports",
				"pkg/auth/staff: the tests that sign in to the staff auth with a password now meet the code step before any session opens; complete it as the skeleton's integration tests do (test/integration/second_factor_test.go: the code read from the mail.Recorder the auth is constructed to send to, entered at user/pending/code/verify)",
			},
			wantSource: []string{
				"\t}\n\n\t// The sign-in rule: every session carries a second factor (requireSecondFactor).\n\ts.SetSignInPolicy(requireSecondFactor)\n\tsession.PasswordSignIn(s)\n",
				"// requireSecondFactor is the sign-in rule every session ends with",
				"func requireSecondFactor(context.Context, *cloudspanner.ReadWriteTransaction, *sessioninfo.NewSessionRequest) (*signin.Decision, error) {\n\treturn signin.RequireMFA(sessioninfo.MethodEmailCode), nil\n}\n",
			},
		},
		{
			name:   "the tenant policy: the rule is chained last and the function written",
			policy: "\ts.SetSignInPolicy(signin.TenantPolicy(nil, nil))",
			wantDetails: []string{
				"pkg/auth/staff/staff.go:28: .Then(requireSecondFactor) chained on the staff auth's sign-in rule, so a sign-in the rule admits waits for the second factor",
				"pkg/auth/staff/staff.go: requireSecondFactor written, the rule returning signin.RequireMFA(sessioninfo.MethodEmailCode), with its imports",
				"pkg/auth/staff: the tests that sign in to the staff auth with a password now meet the code step before any session opens; complete it as the skeleton's integration tests do (test/integration/second_factor_test.go: the code read from the mail.Recorder the auth is constructed to send to, entered at user/pending/code/verify)",
			},
			wantSource: []string{"\ts.SetSignInPolicy(signin.TenantPolicy(nil, nil).Then(requireSecondFactor))\n", "func requireSecondFactor("},
		},
		{
			name:   "a rule function of another name is chained, and none is written",
			policy: "\ts.SetSignInPolicy(signin.TenantPolicy(nil, nil).Then(allowEveryone))",
			funcs: allowFunc + `
func secondFactor(context.Context, *cloudspanner.ReadWriteTransaction, *sessioninfo.NewSessionRequest) (*signin.Decision, error) {
	return signin.RequireMFA(sessioninfo.MethodEmailCode), nil
}
`,
			wantDetails: []string{"pkg/auth/staff/staff.go:28: .Then(secondFactor) chained on the staff auth's sign-in rule, so a sign-in the rule admits waits for the second factor", "pkg/auth/staff: the tests that sign in to the staff auth with a password now meet the code step before any session opens; complete it as the skeleton's integration tests do (test/integration/second_factor_test.go: the code read from the mail.Recorder the auth is constructed to send to, entered at user/pending/code/verify)"},
			wantSource:  []string{"\ts.SetSignInPolicy(signin.TenantPolicy(nil, nil).Then(allowEveryone).Then(secondFactor))\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := map[string]string{"pkg/auth/staff/staff.go": fmt.Sprintf(ruledAuth, tt.policy, tt.funcs)}
			a := discoverFiles(t, files)
			got := secondFactor{}.Run(context.Background(), &Env{App: a, Fix: true})
			want := Result{Name: "second-factor", Status: Pass, Summary: "1 auth(s) end their sign-in rule with the second factor: staff", Details: tt.wantDetails}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
			src, err := os.ReadFile(a.Abs("pkg/auth/staff/staff.go"))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.wantSource {
				if !strings.Contains(string(src), want) {
					t.Errorf("the edited file lacks %q:\n%s", want, src)
				}
			}
			if strings.Count(string(src), "func requireSecondFactor(") > 1 {
				t.Errorf("the rule is written twice:\n%s", src)
			}
			// The edit holds: a second run has nothing to do.
			again := secondFactor{}.Run(context.Background(), &Env{App: discoverFiles(t, nil, a.Root)})
			if again.Status != Pass || len(again.Details) != 0 {
				t.Errorf("the edited package fails the check: %+v", again)
			}
		})
	}
}

// discoverFiles writes the files under a fresh module root (or discovers root again when
// one is given and files are nil) and discovers the application.
func discoverFiles(t *testing.T, files map[string]string, root ...string) *app.App {
	t.Helper()

	dir := ""
	if len(root) > 0 {
		dir = root[0]
	}
	if dir == "" {
		dir = t.TempDir()
		write := func(rel, content string) {
			t.Helper()
			if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		write("go.mod", "module example.com/harbor\n\ngo 1.26.6\n")
		for rel, content := range files {
			write(rel, content)
		}
	}
	a, err := app.Discover(dir)
	if err != nil {
		t.Fatalf("app.Discover() error = %v", err)
	}

	return a
}
