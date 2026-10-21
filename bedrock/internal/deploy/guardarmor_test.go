package deploy

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
)

// armorRepo is the repository the Cloud Armor guard reads: master at c4, whose tree
// carries the stack's terraform.tfvars given (none when empty), and the patch line
// patch/0.1.x at c3, whose tree has prd's policy enforced.
func armorRepo(master string) *githubtest.Repo {
	repo := &githubtest.Repo{
		Refs:  map[string]github.Object{"refs/heads/master": {Type: "commit", SHA: "c4"}, "refs/heads/patch/0.1.x": {Type: "commit", SHA: "c3"}},
		Trees: map[string]string{"c3": "t3", "c4": "t4"},
		Files: map[string]string{"t3:" + tfvarsPath: armorTfvars(`prd = "enforce"`)},
	}
	if master != "" {
		repo.Files["t4:"+tfvarsPath] = master
	}

	return repo
}

// armorTfvars is a stack's terraform.tfvars, every secret pinned, with the cloud_armor
// entries given.
func armorTfvars(entries string) string {
	return pinnedTfvars(allPinned, allPinned, allPinned) + "\ncloud_armor = {\n  " + entries + "\n}\n"
}

// TestGuardCloudArmor: a pull request that removes an environment's cloud_armor entry
// while the base branch, read through GitHub, has the policy on is refused with the
// check's sentence and the refusal is said on the pull request; the two steps that take
// the entry out pass; a tag build, a base branch without the file and a build without
// the repository's token have nothing to compare.
func TestGuardCloudArmor(t *testing.T) {
	t.Parallel()

	const (
		refused   = `Build REJECTED: cloud_armor in terraform.tfvars removes prd in one step: at master the environment is "enforce", and removing the entry detaches the policy from the backend services and destroys it in one apply, which fails while the policy is attached; set it to "off" first (the policy kept, detached), merge and apply, then remove the entry`
		noRemoval = "Cloud Armor guard passed: no policy removed in one step against master."
	)
	noArmor := pinnedTfvars(allPinned, allPinned, allPinned)
	tests := []struct {
		name string
		subs map[string]string
		// noToken is a build whose repository token could not be minted.
		noToken bool
		// tree is the checkout's terraform.tfvars, none when empty; base is the base
		// branch's, as the stand-in serves it on master, none when empty.
		tree, base string
		wantOut    []string
		wantNotOut []string
		// wantNoCalls says nothing was asked of GitHub.
		wantNoCalls bool
		wantErr     string
		wantComment string
	}{
		{
			name:        "a tag build has no base branch and nothing to guard",
			subs:        tagSubs(),
			tree:        noArmor,
			base:        armorTfvars(`prd = "enforce"`),
			wantNotOut:  []string{"Cloud Armor"},
			wantNoCalls: true,
		},
		{
			name:        "a pull request that removes an environment on at the base branch is refused and said on the pull request",
			subs:        jobSubs(),
			tree:        noArmor,
			base:        armorTfvars(`prd = "enforce"`),
			wantOut:     []string{refused, "Posted on pull request 7."},
			wantErr:     refused,
			wantComment: "The pull-request build stopped before the stack's plans (build b-1): " + refused,
		},
		{
			name:        "every environment removed while on is named, in the base branch's order",
			subs:        jobSubs(),
			tree:        noArmor,
			base:        armorTfvars("stg = \"preview\"\n  prd = \"enforce\""),
			wantOut:     []string{`Build REJECTED: cloud_armor in terraform.tfvars removes stg in one step: at master the environment is "preview"`, refused},
			wantErr:     `removes stg in one step: at master the environment is "preview", and removing the entry detaches the policy from the backend services and destroys it in one apply, which fails while the policy is attached; set it to "off" first (the policy kept, detached), merge and apply, then remove the entry; cloud_armor in terraform.tfvars removes prd in one step`,
			wantComment: "removes stg in one step",
		},
		{
			name:    "a pull request that keeps the entry passes",
			subs:    jobSubs(),
			tree:    armorTfvars(`prd = "enforce"`),
			base:    armorTfvars(`prd = "enforce"`),
			wantOut: []string{noRemoval},
		},
		{
			name:    "the first step, on to off, passes",
			subs:    jobSubs(),
			tree:    armorTfvars(`prd = "off"`),
			base:    armorTfvars(`prd = "enforce"`),
			wantOut: []string{noRemoval},
		},
		{
			name:    "the second step, an entry at off on the base branch removed, passes",
			subs:    jobSubs(),
			tree:    noArmor,
			base:    armorTfvars(`prd = "off"`),
			wantOut: []string{noRemoval},
		},
		{
			name:    "a base branch without the file has nothing to compare",
			subs:    jobSubs(),
			tree:    noArmor,
			wantOut: []string{"Cloud Armor guard: master has no infrastructure/terraform.tfvars, so no policy was on there; nothing to compare."},
		},
		{
			name:        "a checkout without the file removes every entry",
			subs:        jobSubs(),
			base:        armorTfvars(`prd = "enforce"`),
			wantOut:     []string{refused},
			wantErr:     refused,
			wantComment: refused,
		},
		{
			name: "a pull request against a patch line is compared with the line, not the default branch",
			subs: func() map[string]string {
				subs := jobSubs()
				subs[baseBranchSub] = "patch/0.1.x"

				return subs
			}(),
			tree:        noArmor,
			base:        armorTfvars(`prd = "off"`),
			wantOut:     []string{`Build REJECTED: cloud_armor in terraform.tfvars removes prd in one step: at patch/0.1.x the environment is "enforce"`},
			wantErr:     "at patch/0.1.x",
			wantComment: `at patch/0.1.x the environment is "enforce"`,
		},
		{
			name:       "without the repository's token nothing is compared, said in the log",
			subs:       jobSubs(),
			noToken:    true,
			tree:       noArmor,
			base:       armorTfvars(`prd = "enforce"`),
			wantOut:    []string{"Cloud Armor guard: without the repository's token the copy of infrastructure/terraform.tfvars on master is not read, and nothing is compared."},
			wantNotOut: []string{"Build REJECTED"},
		},
		{
			name:    "a base branch map that is not written out is an error naming the branch",
			subs:    jobSubs(),
			tree:    noArmor,
			base:    noArmor + "\ncloud_armor = var.armor\n",
			wantErr: "cloud_armor in terraform.tfvars at master is not a map written out",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := map[string]string{}
			if tt.tree != "" {
				files[tfvarsPath] = tt.tree
			}
			w := workspaceFiles(t, files)
			repo := armorRepo(tt.base)
			srv, gh := githubStandIn(t, repo)
			clients := &Clients{GitHub: gh}
			env := map[string]string{githubTokenFact: "test-token"}
			if tt.noToken {
				env = map[string]string{}
			}
			var out strings.Builder
			err := guardCloudArmor(t.Context(), clients, w, &Build{ID: "b-1", Substitutions: tt.subs}, env, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("guardCloudArmor() error = %v, want %q\n%s", err, tt.wantErr, out.String())
				}
			} else if err != nil {
				t.Fatalf("guardCloudArmor() error = %v\n%s", err, out.String())
			}
			containsAll(t, out.String(), tt.wantOut...)
			for _, s := range tt.wantNotOut {
				if strings.Contains(out.String(), s) {
					t.Errorf("output holds %q, want it absent:\n%s", s, out.String())
				}
			}
			if tt.wantNoCalls && len(srv.Calls) != 0 {
				t.Errorf("GitHub was asked %q, want nothing", srv.Calls)
			}
			comments := repo.Comments[7]
			if tt.wantComment == "" && len(comments) > 0 {
				t.Errorf("posted %q, want nothing", comments[0].Body)
			}
			if tt.wantComment != "" && (len(comments) != 1 || !strings.Contains(comments[0].Body, tt.wantComment)) {
				t.Errorf("comments = %+v, want one holding %q", comments, tt.wantComment)
			}
		})
	}
}
