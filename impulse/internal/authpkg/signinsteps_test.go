package authpkg

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestSignInSteps holds the sign-in steps the generated server asks the login page for
// together: the committed file the CI script reads equals SignInSteps (run with
// IMPULSE_UPDATE_AUTHPKG=1 to rewrite it), the list names the library's confirmation, and
// its second-factor entries are exactly the factors requireSecondFactor requires in the
// auth template, so a factor added to the policy is added to the list, and the page's
// declaration is held to it.
func TestSignInSteps(t *testing.T) {
	t.Parallel()

	const file = "sign-in-steps.json"
	want, err := json.Marshal(SignInSteps)
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	if os.Getenv("IMPULSE_UPDATE_AUTHPKG") != "" {
		if err := os.WriteFile(file, want, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var fromFile []string
	if err := json.Unmarshal(got, &fromFile); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	if !slices.Equal(fromFile, SignInSteps) {
		t.Errorf("%s holds %v and SignInSteps is %v: run with IMPULSE_UPDATE_AUTHPKG=1 to rewrite the file", file, fromFile, SignInSteps)
	}

	if !slices.Contains(SignInSteps, "confirmation") {
		t.Errorf("SignInSteps = %v names no confirmation, the library's password confirmation every server may hold a sign-in for", SignInSteps)
	}

	// The factors the policy requires, as the template names them, to the step names the
	// server's pending status reports them under.
	factorNames := map[string]string{"sessioninfo.MethodEmailCode": "email-code"}
	policy := regexp.MustCompile(`func requireSecondFactor\([^{]*\{\s*return signin\.RequireMFA\(([^)]*)\), nil`).FindStringSubmatch(authTemplate)
	if policy == nil {
		t.Fatal("authTemplate writes no requireSecondFactor returning signin.RequireMFA(...)")
	}
	var required []string
	for _, arg := range strings.Split(policy[1], ",") {
		arg = strings.TrimSpace(arg)
		name, ok := factorNames[arg]
		if !ok {
			t.Fatalf("requireSecondFactor requires %s, which this test does not know the pending status's name of: add it to factorNames", arg)
		}
		required = append(required, "mfa:"+name)
	}
	var listed []string
	for _, step := range SignInSteps {
		if strings.HasPrefix(step, "mfa:") {
			listed = append(listed, step)
		}
	}
	if !slices.Equal(listed, required) {
		t.Errorf("SignInSteps names the second-factor steps %v and requireSecondFactor requires %v", listed, required)
	}
}
