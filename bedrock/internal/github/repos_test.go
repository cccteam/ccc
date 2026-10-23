package github_test

import (
	"context"
	"slices"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
)

func TestArchive(t *testing.T) {
	t.Parallel()

	s := githubtest.New(t)
	s.Repos["imp-example/harbor"] = &githubtest.Repo{}
	c := s.Client()
	if err := c.Archive(context.Background(), "imp-example", "harbor"); err != nil {
		t.Fatalf("Archive() = %v", err)
	}
	if !s.Repos["imp-example/harbor"].Archived {
		t.Error("the repository is not archived")
	}
	if !slices.Contains(s.Calls, "PATCH /repos/imp-example/harbor") {
		t.Errorf("calls = %v, want the repository's PATCH", s.Calls)
	}
	err := c.Archive(context.Background(), "imp-example", "beacon")
	if !github.NotFound(err) {
		t.Errorf("Archive() of a repository the organization lacks = %v, want not found", err)
	}
}
