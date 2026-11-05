package github_test

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/github"
)

// TestIssuedByApp: the tokens GitHub's apps are issued are told by their prefix; a
// personal access token, classic or fine-grained, is not among them.
func TestIssuedByApp(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ token, want string }{
		{"ghu_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijkl", "a GitHub App's user access token"},
		{"ghs_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijkl", "a GitHub App's installation access token"},
		{"gho_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijkl", "an OAuth app's access token"},
		{"ghr_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijkl", "a GitHub App's refresh token"},
		{"ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijkl", ""},
		{"github_pat_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijkl", ""},
		{"test-token", ""},
		{"", ""},
	} {
		got := github.IssuedByApp(tc.token)
		if tc.want == "" && got != "" {
			t.Errorf("IssuedByApp(%q) = %q, want none", tc.token, got)
		}
		if tc.want != "" && !strings.HasPrefix(got, tc.want) {
			t.Errorf("IssuedByApp(%q) = %q, want it to start with %q", tc.token, got, tc.want)
		}
	}
}
