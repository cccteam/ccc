package check

import (
	"context"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/authpkg"
)

// secondFactor verifies that every auth package ends its sign-in rule with the second
// factor: the one SetSignInPolicy call on the Auth its New constructs (the session
// library panics at a second) is given requireSecondFactor, a function of the package
// returning signin.RequireMFA, alone or chained last with .Then after the tenant policy.
// Every session carries a second factor, and the rule is the whole of it: the sign-in
// codes and trusted devices tables (session-tables holds those) and the pending sign-in's
// code handlers (the router compiles against them) ask nobody for the code by themselves,
// so an auth that sets no rule, or a rule that ends elsewhere, admits a session on the
// password alone, and nothing else reads it. Under --fix the rule is written as impulse
// upgrade's second-factor recipe writes it.
type secondFactor struct{}

const secondFactorName = "second-factor"

func (secondFactor) Name() string { return secondFactorName }

func (secondFactor) Describe() string {
	return "every auth package's New ends its sign-in rule with the second factor: its one SetSignInPolicy call is given requireSecondFactor (a function of the package returning signin.RequireMFA), alone or chained last with .Then after the tenant policy"
}

// SecondFactorMeaning says what the rule means in this framework, for the brief of the
// check and of the recipe that writes it.
const SecondFactorMeaning = "Every session carries a second factor. An auth package's New sets its Auth's sign-in rule once (SetSignInPolicy; the session library panics at a second) and the rule ends with requireSecondFactor, a function of the package returning signin.RequireMFA(sessioninfo.MethodEmailCode), chained last with .Then where the tenant policy comes first: a sign-in the password, or a directory the deployment does not administer, admitted then waits for the code the auth mails, unless a browser the person asked to be remembered or the directory's attestation stands in. The sign-in codes and trusted devices tables and the pending sign-in's code handlers ask nobody by themselves. impulse upgrade's second-factor recipe and impulse check --fix write the rule; the skeleton's auth package is the pattern."

func (secondFactor) Meaning() string { return SecondFactorMeaning }

func (c secondFactor) Run(_ context.Context, env *Env) Result {
	a := env.App
	if len(a.AuthPackages) == 0 {
		return skip(c.Name(), "no auth package")
	}
	rules, err := AuthRules(a)
	if err != nil {
		return fail(c.Name(), err.Error())
	}
	var details, fixed, held []string
	failing := 0
	for i := range rules {
		r := &rules[i]
		switch {
		case r.Auth == nil:
			continue // auths-wired reports a package constructing no Auth
		case r.Ends():
		case !env.Fix:
			details = append(details, r.Findings()...)
			failing++

			continue
		default:
			did, skipped, err := AddSecondFactorRule(a, r)
			if err != nil {
				return fail(c.Name(), err.Error())
			}
			if skipped != "" {
				details = append(details, skipped)
				failing++

				continue
			}
			fixed = append(fixed, did...)
			fixed = append(fixed, SecondFactorTestObligation(r.Package))
		}
		held = append(held, r.Package.Name)
	}
	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d auth(s) without the second factor", failing), details...)
	}
	summary := fmt.Sprintf("%d auth(s) end their sign-in rule with the second factor: %s", len(held), strings.Join(held, ", "))
	if len(fixed) > 0 {
		return passWithDetails(c.Name(), summary, fixed...)
	}

	return pass(c.Name(), summary)
}

// SecondFactorTestObligation says what writing the rule into an auth package leaves to
// its author, which no check sees: the application's tests that sign in to the auth with
// a password now meet the code step before any session opens, and complete it the way
// the skeleton's integration tests do.
func SecondFactorTestObligation(p *app.AuthPackage) string {
	return fmt.Sprintf("%s: the tests that sign in to the %s auth with a password now meet the code step before any session opens; complete it as the skeleton's integration tests do (test/integration/second_factor_test.go: the code read from the mail.Recorder the auth is constructed to send to, entered at user/pending/code/verify)", p.Dir, p.Name)
}

// SecondFactorLeftNote is the sentence over the obligations the rule leaves.
const SecondFactorLeftNote = "Every password sign-in now waits for the mailed code, so a test that signs in and expects a session completes the code step first; the skeleton's integration tests are the pattern."

// The identifiers the rule is read and written by: the function the auth template
// writes, the session library's setter, the chaining of two policies and the decision.
const (
	ruleFunc      = "requireSecondFactor"
	setPolicy     = "SetSignInPolicy"
	thenMethod    = "Then"
	requireMFA    = "RequireMFA"
	signInPath    = "github.com/cccteam/session/signin"
	infoPath      = "github.com/cccteam/session/sessioninfo"
	spannerPath   = "cloud.google.com/go/spanner"
	spannerAlias  = "cloudspanner"
	contextPath   = "context"
	rulePattern   = "the skeleton's auth package is the pattern"
	ruleWriters   = "impulse upgrade's second-factor recipe or impulse check --fix writes it"
	ruleStatement = "// The sign-in rule: every session carries a second factor (requireSecondFactor)."
)

// AuthRule is what an auth package's source says of its sign-in rule, read for the
// second-factor check and for the recipe that writes the rule: the SetSignInPolicy calls
// on its Auth and the functions of the package returning signin.RequireMFA.
type AuthRule struct {
	Package *app.AuthPackage
	// Auth is the package's Auth construction, nil when the scan sees none in it.
	Auth *app.Auth
	// Calls are the SetSignInPolicy calls in the package's files, in file then source
	// order.
	Calls []PolicyCall
	// Rules names the package-level functions returning signin.RequireMFA, sorted.
	Rules []string
	// After is the offset in the construction's file where a SetSignInPolicy call goes
	// when the package makes none: just past the statement checking the construction's
	// error, or past the construction when no check follows it. -1 when the construction
	// is not found as a statement of the file.
	After int
}

// PolicyCall is one SetSignInPolicy call.
type PolicyCall struct {
	File string
	Line int
	// Receiver is the identifier the call is made on.
	Receiver string
	// Last is the policy the call's rule ends with, as the source spells it: the
	// argument, or the argument of the last .Then.
	Last string
	// Ident is Last when it is a bare identifier, which a function of the package may
	// answer to; empty for a qualified name or any other expression.
	Ident string
	// ArgEnd is the offset in File just past the call's argument, where .Then chains.
	ArgEnd int
}

// Ends reports whether the package's rule ends with the second factor: one call, its
// last policy a function of the package returning signin.RequireMFA.
func (r *AuthRule) Ends() bool {
	return len(r.Calls) == 1 && r.Calls[0].Ident != "" && slices.Contains(r.Rules, r.Calls[0].Ident)
}

// Findings says what keeps the rule from ending with the second factor, one line each.
func (r *AuthRule) Findings() []string {
	if r.Ends() || r.Auth == nil {
		return nil
	}
	if len(r.Calls) == 0 {
		receiver := r.Auth.Var
		if receiver == "" {
			receiver = "authSession"
		}

		return []string{fmt.Sprintf("%s:%d: %s.New sets no sign-in rule on the Auth it constructs, so the password alone admits a session; every session carries a second factor: set %s.%s(%s), %s returning signin.%s(sessioninfo.MethodEmailCode) (%s; %s)", r.Auth.File, r.Auth.Line, r.Package.Name, receiver, setPolicy, ruleFunc, ruleFunc, requireMFA, ruleWriters, rulePattern)}
	}
	var found []string
	first := &r.Calls[0]
	switch {
	case first.Ident == "":
		found = append(found, fmt.Sprintf("%s:%d: the %s auth's sign-in rule ends with %s, which is no function of the package; chain the second factor last: %s.%s(%s) (%s; %s)", first.File, first.Line, r.Package.Name, first.Last, first.Last, thenMethod, ruleFunc, ruleWriters, rulePattern))
	case !slices.Contains(r.Rules, first.Ident):
		found = append(found, fmt.Sprintf("%s:%d: the %s auth's sign-in rule ends with %s, which returns no signin.%s decision, so no sign-in waits for the code; chain the second factor last: %s.%s(%s), %s returning signin.%s(sessioninfo.MethodEmailCode) (%s; %s)", first.File, first.Line, r.Package.Name, first.Ident, requireMFA, first.Ident, thenMethod, ruleFunc, ruleFunc, requireMFA, ruleWriters, rulePattern))
	}
	for i := 1; i < len(r.Calls); i++ {
		c := &r.Calls[i]
		found = append(found, fmt.Sprintf("%s:%d: a second %s call on the %s auth; the session library panics at a second policy: chain it on the first with .%s", c.File, c.Line, setPolicy, r.Package.Name, thenMethod))
	}

	return found
}

// AuthRules reads every auth package's sign-in rule from its source, in package order.
func AuthRules(a *app.App) ([]AuthRule, error) {
	rules := make([]AuthRule, 0, len(a.AuthPackages))
	for i := range a.AuthPackages {
		p := &a.AuthPackages[i]
		r, err := readAuthRule(a, p)
		if err != nil {
			return nil, err
		}
		rules = append(rules, *r)
	}

	return rules, nil
}

// readAuthRule reads one package's non-test files for the policy calls, the rule
// functions and where a call would go.
func readAuthRule(a *app.App, p *app.AuthPackage) (*AuthRule, error) {
	r := &AuthRule{Package: p, Auth: authOf(a, p), After: -1}
	entries, err := os.ReadDir(a.Abs(p.Dir))
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadDir()")
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || isTestFile(e.Name()) {
			continue
		}
		rel := path.Join(p.Dir, e.Name())
		src, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			return nil, errors.Wrap(err, "os.ReadFile()")
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, rel, src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, errors.Wrap(err, "parser.ParseFile()")
		}
		r.Calls = append(r.Calls, policyCalls(fset, f, rel)...)
		r.Rules = append(r.Rules, ruleFuncs(f)...)
		if r.Auth != nil && rel == r.Auth.File {
			r.After = afterConstruction(fset, f, r.Auth.Line)
		}
	}
	slices.Sort(r.Rules)

	return r, nil
}

// policyCalls lists the file's SetSignInPolicy calls.
func policyCalls(fset *token.FileSet, f *ast.File, rel string) []PolicyCall {
	var calls []PolicyCall
	ast.Inspect(f, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := ce.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != setPolicy || len(ce.Args) != 1 {
			return true
		}
		receiver := ""
		if id, ok := sel.X.(*ast.Ident); ok {
			receiver = id.Name
		}
		last := policyEnd(ce.Args[0])
		call := PolicyCall{File: rel, Line: fset.Position(ce.Pos()).Line, Receiver: receiver, Last: types.ExprString(last), ArgEnd: fset.Position(ce.Args[0].End()).Offset}
		if id, ok := last.(*ast.Ident); ok {
			call.Ident = id.Name
		}
		calls = append(calls, call)

		return true
	})

	return calls
}

// policyEnd is the policy a rule ends with: the expression itself, or the argument of
// its last .Then.
func policyEnd(e ast.Expr) ast.Expr {
	ce, ok := e.(*ast.CallExpr)
	if !ok || len(ce.Args) != 1 {
		return e
	}
	if sel, ok := ce.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == thenMethod {
		return policyEnd(ce.Args[0])
	}

	return e
}

// ruleFuncs names the file's package-level functions returning signin.RequireMFA.
func ruleFuncs(f *ast.File) []string {
	signIn := importedAs(f, signInPath)
	var names []string
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv != nil || fd.Body == nil || signIn == "" {
			continue
		}
		returnsMFA := false
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			ret, ok := n.(*ast.ReturnStmt)
			if !ok || len(ret.Results) == 0 {
				return true
			}
			if ce, ok := ret.Results[0].(*ast.CallExpr); ok && isSelector(ce.Fun, signIn, requireMFA) {
				returnsMFA = true
			}

			return true
		})
		if returnsMFA {
			names = append(names, fd.Name.Name)
		}
	}

	return names
}

// afterConstruction is the offset just past the construction's error check (the if
// statement following the assignment at line), or past the assignment when no check
// follows; -1 when no statement of a block starts at line.
func afterConstruction(fset *token.FileSet, f *ast.File, line int) int {
	after := -1
	ast.Inspect(f, func(n ast.Node) bool {
		block, ok := n.(*ast.BlockStmt)
		if !ok || after >= 0 {
			return after < 0
		}
		for i, stmt := range block.List {
			if _, ok := stmt.(*ast.AssignStmt); !ok || fset.Position(stmt.Pos()).Line != line {
				continue
			}
			after = fset.Position(stmt.End()).Offset
			if i+1 < len(block.List) {
				if check, ok := block.List[i+1].(*ast.IfStmt); ok {
					after = fset.Position(check.End()).Offset
				}
			}

			return false
		}

		return true
	})

	return after
}

// AddSecondFactorRule ends the package's sign-in rule with the second factor, as the
// auth template writes it: .Then(requireSecondFactor) chained on the one call's policy,
// or the call set on the construction's identifier after its error check when the
// package makes none, and requireSecondFactor written at the end of the edited file with
// its imports when the package has no function returning signin.RequireMFA (an existing
// one is chained instead). It reports what it did, one line per edit, or why the edit
// could not be made.
func AddSecondFactorRule(a *app.App, r *AuthRule) (did []string, skipped string, err error) {
	if r.Ends() {
		return nil, "", nil
	}
	if reason := unwritable(r); reason != "" {
		return nil, reason, nil
	}
	rule := ruleFunc
	if len(r.Rules) > 0 {
		rule = r.Rules[0]
	}
	rel, at, text := r.Auth.File, r.After, "\n\n\t"+ruleStatement+"\n\t"+r.Auth.Var+"."+setPolicy+"("+rule+")"
	if len(r.Calls) == 1 {
		rel, at, text = r.Calls[0].File, r.Calls[0].ArgEnd, "."+thenMethod+"("+rule+")"
	}
	info, err := os.Stat(a.Abs(rel))
	if err != nil {
		return nil, "", errors.Wrap(err, "os.Stat()")
	}
	src, err := os.ReadFile(a.Abs(rel))
	if err != nil {
		return nil, "", errors.Wrap(err, "os.ReadFile()")
	}
	out := slices.Concat(src[:at], []byte(text), src[at:])
	if len(r.Calls) == 1 {
		did = append(did, fmt.Sprintf("%s:%d: .%s(%s) chained on the %s auth's sign-in rule, so a sign-in the rule admits waits for the second factor", rel, r.Calls[0].Line, thenMethod, rule, r.Package.Name))
	} else {
		did = append(did, fmt.Sprintf("%s:%d: %s.%s(%s) set after the %s auth's construction, so every sign-in waits for the second factor", rel, r.Auth.Line, r.Auth.Var, setPolicy, rule, r.Package.Name))
	}
	if len(r.Rules) == 0 {
		out, err = appendRuleFunc(rel, out)
		if err != nil {
			return nil, "", err
		}
		did = append(did, fmt.Sprintf("%s: %s written, the rule returning signin.%s(sessioninfo.MethodEmailCode), with its imports", rel, ruleFunc, requireMFA))
	}
	out, err = format.Source(out)
	if err != nil {
		return nil, "", errors.Wrapf(err, "format.Source(): %s after writing the sign-in rule", rel)
	}
	if err := os.WriteFile(a.Abs(rel), out, info.Mode().Perm()); err != nil {
		return nil, "", errors.Wrap(err, "os.WriteFile()")
	}

	return did, "", nil
}

// unwritable says why the rule cannot be written into the package, or nothing.
func unwritable(r *AuthRule) string {
	switch {
	case r.Auth == nil:
		return fmt.Sprintf("%s: no Auth construction the scan can see; end the %s auth's sign-in rule with %s by hand (%s)", r.Package.Dir, r.Package.Name, ruleFunc, rulePattern)
	case len(r.Calls) > 1:
		return fmt.Sprintf("%s:%d: a second %s call on the %s auth; chain the policies with .%s and end them with %s by hand (%s)", r.Calls[1].File, r.Calls[1].Line, setPolicy, r.Package.Name, thenMethod, ruleFunc, rulePattern)
	case len(r.Calls) == 0 && r.Auth.Var == "":
		return fmt.Sprintf("%s:%d: the %s auth's construction is assigned to no identifier; set %s(%s) on it by hand (%s)", r.Auth.File, r.Auth.Line, r.Package.Name, setPolicy, ruleFunc, rulePattern)
	case len(r.Calls) == 0 && r.After < 0:
		return fmt.Sprintf("%s:%d: the %s auth's construction is no statement of a block; set %s.%s(%s) after it by hand (%s)", r.Auth.File, r.Auth.Line, r.Package.Name, r.Auth.Var, setPolicy, ruleFunc, rulePattern)
	}

	return ""
}

// appendRuleFunc writes requireSecondFactor at the end of the file under the names the
// file imports its packages by, adding the imports it lacks.
func appendRuleFunc(rel string, src []byte) ([]byte, error) {
	text := authpkg.SecondFactorRule()
	imports := []struct{ name, path, written string }{
		{"", contextPath, "context"},
		{spannerAlias, spannerPath, spannerAlias},
		{"", infoPath, "sessioninfo"},
		{"", signInPath, "signin"},
	}
	for _, imp := range imports {
		local, err := app.ImportName(rel, src, imp.path)
		if err != nil {
			return nil, err
		}
		switch local {
		case "":
			if src, err = app.AddNamedImport(rel, src, imp.name, imp.path); err != nil {
				return nil, err
			}
		case imp.written:
		default:
			text = strings.ReplaceAll(text, imp.written+".", local+".")
		}
	}

	return append(append(src, '\n'), []byte(text)...), nil
}
