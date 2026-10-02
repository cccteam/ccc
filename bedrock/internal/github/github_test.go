package github_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
)

func TestClient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// call runs one method over the client and returns what to compare.
		call    func(ctx context.Context, c *github.Client) (string, error)
		want    string
		wantErr string
		// notFound is true when the error must satisfy NotFound.
		notFound bool
	}{
		{
			name: "a lightweight tag names its commit",
			call: func(ctx context.Context, c *github.Client) (string, error) {
				return c.TagCommit(ctx, "acme", "quill", "v0.1.0")
			},
			want: "c1",
		},
		{
			name: "an annotated tag is resolved through its tag object",
			call: func(ctx context.Context, c *github.Client) (string, error) {
				return c.TagCommit(ctx, "acme", "quill", "v0.2.0")
			},
			want: "c3",
		},
		{
			name: "an absent tag is not found",
			call: func(ctx context.Context, c *github.Client) (string, error) {
				return c.TagCommit(ctx, "acme", "quill", "v9.9.9")
			},
			wantErr: "answered 404: Not Found", notFound: true,
		},
		{
			name: "a comparison reports ahead and the merge base",
			call: func(ctx context.Context, c *github.Client) (string, error) {
				cmp, err := c.Compare(ctx, "acme", "quill", "c1", "main")
				if err != nil {
					return "", err
				}

				return cmp.Status + " " + cmp.MergeBaseCommit.SHA, nil
			},
			want: "ahead c1",
		},
		{
			name: "the token's user",
			call: func(ctx context.Context, c *github.Client) (string, error) {
				return c.User(ctx)
			},
			want: "octocat",
		},
		{
			name: "a workflow is dispatched on a branch with its inputs",
			call: func(ctx context.Context, c *github.Client) (string, error) {
				err := c.DispatchWorkflow(ctx, "acme", "quill", "operations.yml", "main", map[string]string{"environment": "tst", "release": "v0.1.1"})

				return "dispatched", err
			},
			want: "dispatched",
		},
		{
			name: "a workflow dispatched on a branch the repository lacks is refused",
			call: func(ctx context.Context, c *github.Client) (string, error) {
				return "", c.DispatchWorkflow(ctx, "acme", "quill", "operations.yml", "nowhere", nil)
			},
			wantErr: "answered 422: No ref found for: nowhere",
		},
		{
			name: "the tags list every tag with its commit",
			call: func(ctx context.Context, c *github.Client) (string, error) {
				tags, err := c.Tags(ctx, "acme", "quill")
				if err != nil {
					return "", err
				}
				var out []string
				for _, tag := range tags {
					out = append(out, tag.Name+"@"+tag.Commit.SHA)
				}

				return strings.Join(out, ","), nil
			},
			want: "v0.1.0@c1,v0.1.1@c2,v0.2.0@c3",
		},
		{
			name: "file contents at a ref",
			call: func(ctx context.Context, c *github.Client) (string, error) {
				data, err := c.Contents(ctx, "acme", "quill", ".release-please-manifest.json", "c1")
				if err != nil {
					return "", err
				}

				return strings.TrimSpace(string(data)), nil
			},
			want: `{".": "0.1.0"}`,
		},
		{
			name: "a refusal carries the status and the message",
			call: func(ctx context.Context, c *github.Client) (string, error) {
				_, err := github.New(c.Base(), "wrong").Tags(ctx, "acme", "quill")

				return "", err
			},
			wantErr: "GitHub GET /repos/acme/quill/tags?per_page=100&page=1 answered 401: Bad credentials",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := githubtest.New(t)
			repo := server.AddRepo("acme", "quill", &githubtest.Repo{
				Refs: map[string]github.Object{
					"refs/heads/main":  {Type: "commit", SHA: "c3"},
					"refs/tags/v0.1.0": {Type: "commit", SHA: "c1"},
					"refs/tags/v0.1.1": {Type: "commit", SHA: "c2"},
					"refs/tags/v0.2.0": {Type: "tag", SHA: "t3"},
				},
				TagObjects: map[string]string{"t3": "c3"},
				Ancestry:   map[string][]string{"c1": {"c1"}, "c2": {"c2", "c1"}, "c3": {"c3", "c2", "c1"}},
				Trees:      map[string]string{"c1": "tree1"},
				Files:      map[string]string{"tree1:.release-please-manifest.json": `{".": "0.1.0"}` + "\n"},
			})
			_ = repo
			got, err := tt.call(t.Context(), server.Client())
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, wantErr %q", err, tt.wantErr)
				}
				if tt.notFound && !github.NotFound(err) {
					t.Errorf("NotFound(%v) = false, want true", err)
				}

				return
			}
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIssueComments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		count int
		want  int
		// wantCalls are the requests the read makes, in order.
		wantCalls []string
	}{
		{
			name:      "a short thread is one page",
			count:     7,
			want:      7,
			wantCalls: []string{"GET /repos/acme/quill/issues/7", "GET /repos/acme/quill/issues/7/comments"},
		},
		{
			name:      "a long thread reads its last page",
			count:     250,
			want:      50,
			wantCalls: []string{"GET /repos/acme/quill/issues/7", "GET /repos/acme/quill/issues/7/comments"},
		},
		{
			name:      "a last page under five comments takes the page before it too",
			count:     203,
			want:      103,
			wantCalls: []string{"GET /repos/acme/quill/issues/7", "GET /repos/acme/quill/issues/7/comments", "GET /repos/acme/quill/issues/7/comments"},
		},
		{
			name:      "no comments, no page",
			count:     0,
			want:      0,
			wantCalls: []string{"GET /repos/acme/quill/issues/7"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := githubtest.New(t)
			comments := make([]github.Comment, 0, tt.count)
			for i := 1; i <= tt.count; i++ {
				comments = append(comments, github.Comment{ID: int64(i), Body: fmt.Sprintf("comment %d", i)})
			}
			srv.AddRepo("acme", "quill", &githubtest.Repo{Comments: map[int][]github.Comment{7: comments}})
			got, err := srv.Client().IssueComments(t.Context(), "acme", "quill", 7)
			if err != nil {
				t.Fatalf("IssueComments() error = %v", err)
			}
			if len(got) != tt.want {
				t.Fatalf("IssueComments() = %d comments, want %d", len(got), tt.want)
			}
			if tt.want > 0 {
				if first, last := got[0].Body, got[len(got)-1].Body; first != fmt.Sprintf("comment %d", tt.count-tt.want+1) || last != fmt.Sprintf("comment %d", tt.count) {
					t.Errorf("IssueComments() spans %q to %q, want the latest %d in order", first, last, tt.want)
				}
			}
			if diff := cmp.Diff(tt.wantCalls, srv.Calls); diff != "" {
				t.Errorf("calls mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
