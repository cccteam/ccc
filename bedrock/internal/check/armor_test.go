package check

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// armorRepo is a temporary repository on master whose infrastructure/terraform.tfvars
// carries a cloud_armor map.
type armorRepo struct {
	t    *testing.T
	root string
}

func newArmorRepo(t *testing.T) *armorRepo {
	t.Helper()
	r := &armorRepo{t: t, root: t.TempDir()}
	r.git("init", "-q", "-b", "master")

	return r
}

func (r *armorRepo) git(args ...string) {
	r.t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", r.root, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// stack is the stack directory, infrastructure under the root.
func (r *armorRepo) stack() string {
	return filepath.Join(r.root, "infrastructure")
}

// write puts terraform.tfvars in the working tree with the cloud_armor map given, or no
// map when empty.
func (r *armorRepo) write(armor string) {
	r.t.Helper()
	if err := os.MkdirAll(r.stack(), 0o750); err != nil {
		r.t.Fatal(err)
	}
	src := "secret_versions = {\n  prd = {}\n}\n"
	if armor != "" {
		src += "\n" + armor
	}
	if err := os.WriteFile(filepath.Join(r.stack(), tfvarsFile), []byte(src), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

// commit writes the map and commits it on master.
func (r *armorRepo) commit(armor string) {
	r.t.Helper()
	r.write(armor)
	r.git("add", "-A")
	r.git("commit", "-q", "-m", "stack")
}

func TestScanCloudArmor(t *testing.T) {
	t.Parallel()

	const (
		on  = "cloud_armor = {\n  stg = \"preview\"\n  prd = \"enforce\"\n}\n"
		off = "cloud_armor = {\n  stg = \"preview\"\n  prd = \"off\"\n}\n"
	)
	tests := []struct {
		name string
		// committed is the map on master; none when empty, and noFile commits no
		// terraform.tfvars at all.
		committed string
		noFile    bool
		// working is the map in the tree; none when empty.
		working string
		// noRepo runs the scan over a plain directory.
		noRepo  bool
		branch  string
		want    []CloudArmorFinding
		wantErr string
	}{
		{
			name:      "an environment enforced on master and removed in the tree is refused",
			committed: on,
			working:   "cloud_armor = {\n  stg = \"preview\"\n}\n",
			want:      []CloudArmorFinding{{Environment: "prd", Mode: "enforce", Ref: "refs/heads/master"}},
		},
		{
			name:      "every entry removed names each environment that was on, in the file's order",
			committed: on,
			want:      []CloudArmorFinding{{Environment: "stg", Mode: "preview", Ref: "refs/heads/master"}, {Environment: "prd", Mode: "enforce", Ref: "refs/heads/master"}},
		},
		{
			name:      "the first step, on to off, passes",
			committed: on,
			working:   off,
		},
		{
			name:      "the second step, off removed, passes",
			committed: off,
			working:   "cloud_armor = {\n  stg = \"preview\"\n}\n",
		},
		{
			name:      "an entry added or kept passes",
			committed: "cloud_armor = {\n  stg = \"preview\"\n}\n",
			working:   on,
		},
		{
			name:    "master without the map has nothing on to remove",
			working: "cloud_armor = {\n}\n",
		},
		{
			name:    "master without the file has nothing to compare",
			noFile:  true,
			working: "cloud_armor = {\n}\n",
		},
		{
			name:      "a repository without the default branch says nothing",
			committed: on,
			branch:    "main",
		},
		{
			name:    "a directory outside a repository says nothing",
			noRepo:  true,
			working: on,
		},
		{
			name:      "a map that is not written out is refused",
			committed: on,
			working:   "cloud_armor = var.armor\n",
			wantErr:   "cloud_armor in",
		},
		{
			name:      "a mode that is not a string is refused",
			committed: on,
			working:   "cloud_armor = {\n  prd = 1\n}\n",
			wantErr:   "cloud_armor.prd in",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			branch := tt.branch
			if branch == "" {
				branch = "master"
			}
			var dir string
			if tt.noRepo {
				r := &armorRepo{t: t, root: t.TempDir()}
				r.write(tt.working)
				dir = r.stack()
			} else {
				r := newArmorRepo(t)
				if tt.noFile {
					if err := os.WriteFile(filepath.Join(r.root, "README.md"), []byte("x\n"), 0o600); err != nil {
						t.Fatal(err)
					}
					r.git("add", "-A")
					r.git("commit", "-q", "-m", "no stack")
				} else {
					r.commit(tt.committed)
				}
				r.write(tt.working)
				dir = r.stack()
			}
			got, err := scanCloudArmor(t.Context(), dir, branch)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("scanCloudArmor() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("scanCloudArmor() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("scanCloudArmor() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestScanCloudArmorOrigin: origin's copy of the default branch is read before the local
// one, as the pull-request build compares against it.
func TestScanCloudArmorOrigin(t *testing.T) {
	t.Parallel()

	r := newArmorRepo(t)
	r.commit("cloud_armor = {\n  prd = \"enforce\"\n}\n")
	r.git("update-ref", "refs/remotes/origin/master", "HEAD")
	r.commit("cloud_armor = {\n  prd = \"off\"\n}\n")
	r.write("")
	got, err := scanCloudArmor(t.Context(), r.stack(), "master")
	if err != nil {
		t.Fatalf("scanCloudArmor() error = %v", err)
	}
	want := []CloudArmorFinding{{Environment: "prd", Mode: "enforce", Ref: "refs/remotes/origin/master"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("scanCloudArmor() mismatch (-want +got):\n%s", diff)
	}
}

// TestRunCloudArmor: the finding fails the check and its line names the environment,
// the ref, the mode and the two steps.
func TestRunCloudArmor(t *testing.T) {
	t.Parallel()

	dir := copyGolden(t)
	root := filepath.Dir(dir)
	cmd := func(args ...string) {
		t.Helper()
		c := exec.CommandContext(context.Background(), "git", append([]string{"-C", root, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	path := filepath.Join(dir, tfvarsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const empty = "cloud_armor = {\n}\n"
	if !bytes.Contains(data, []byte(empty)) {
		t.Fatalf("the golden terraform.tfvars has no empty cloud_armor map:\n%s", data)
	}
	if err := os.WriteFile(path, bytes.Replace(data, []byte(empty), []byte("cloud_armor = {\n  prd = \"preview\"\n}\n"), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd("init", "-q", "-b", "master")
	cmd("add", "-A")
	cmd("commit", "-q", "-m", "stack")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Run(t.Context(), harborModel(t), dir, filepath.Join(dir, "root"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.Clean() {
		t.Error("Clean() = true, want false: a policy removed in one step is refused")
	}
	var out bytes.Buffer
	report.Write(&out)
	want := `refused  cloud_armor in terraform.tfvars removes prd in one step: at refs/heads/master the environment is "preview", and removing the entry detaches the policy from the backend services and destroys it in one apply, which fails while the policy is attached; set it to "off" first (the policy kept, detached), merge and apply, then remove the entry`
	if !strings.Contains(out.String(), want) {
		t.Errorf("Write() output lacks %q:\n%s", want, out.String())
	}
}

// TestScanSignInThreshold reads the sign-in throttle's variables: a count or an interval
// that is not a number, not whole, or not above zero is refused naming the variable and
// the value as written, the count's first; a variable left out, and no file, pass.
func TestScanSignInThreshold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// working is what follows the secret_versions map in terraform.tfvars; noFile
		// writes no file at all.
		working string
		noFile  bool
		want    []SignInThresholdFinding
		wantErr string
	}{
		{name: "neither variable written passes"},
		{name: "no file passes", noFile: true},
		{name: "positive whole numbers pass", working: "cloud_armor_sign_in_count    = 10\ncloud_armor_sign_in_interval = 300\n"},
		{name: "a count of zero is refused", working: "cloud_armor_sign_in_count = 0\n", want: []SignInThresholdFinding{{Variable: "cloud_armor_sign_in_count", Value: "0"}}},
		{name: "a negative interval is refused", working: "cloud_armor_sign_in_interval = -60\n", want: []SignInThresholdFinding{{Variable: "cloud_armor_sign_in_interval", Value: "-60"}}},
		{name: "a fraction is refused", working: "cloud_armor_sign_in_count = 2.5\n", want: []SignInThresholdFinding{{Variable: "cloud_armor_sign_in_count", Value: "2.5"}}},
		{name: "a string is refused", working: "cloud_armor_sign_in_interval = \"60\"\n", want: []SignInThresholdFinding{{Variable: "cloud_armor_sign_in_interval", Value: "\"60\""}}},
		{
			name:    "both refused, the count first",
			working: "cloud_armor_sign_in_interval = 0\ncloud_armor_sign_in_count    = -1\n",
			want:    []SignInThresholdFinding{{Variable: "cloud_armor_sign_in_count", Value: "-1"}, {Variable: "cloud_armor_sign_in_interval", Value: "0"}},
		},
		{name: "a value that is not a literal is an error", working: "cloud_armor_sign_in_count = var.n\n", wantErr: "cloud_armor_sign_in_count in"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &armorRepo{t: t, root: t.TempDir()}
			if !tt.noFile {
				r.write(tt.working)
			}
			got, err := scanSignInThreshold(r.stack())
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("scanSignInThreshold() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("scanSignInThreshold() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("scanSignInThreshold() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestRunSignInThreshold: a threshold the policy could not take fails the check, and its
// line names the variable, the value and what a value of it is.
func TestRunSignInThreshold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		extra string
		want  string
	}{
		{
			name:  "a count of zero",
			extra: "\ncloud_armor_sign_in_count = 0\n",
			want:  "refused  cloud_armor_sign_in_count in terraform.tfvars is 0; the sign-in throttle's count is a positive whole number: the POSTs one client address may make to a sign-in route in the interval before its further ones answer 429",
		},
		{
			name:  "an interval of a fraction",
			extra: "\ncloud_armor_sign_in_interval = 0.5\n",
			want:  "refused  cloud_armor_sign_in_interval in terraform.tfvars is 0.5; the sign-in throttle's interval is a positive whole number of seconds: how long the count is taken over",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := copyGolden(t)
			path := filepath.Join(dir, tfvarsFile)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(data, []byte(tt.extra)...), 0o600); err != nil {
				t.Fatal(err)
			}
			report, err := Run(t.Context(), harborModel(t), dir, filepath.Join(dir, "root"))
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if report.Clean() {
				t.Error("Clean() = true, want false: a threshold the policy could not take is refused")
			}
			var out bytes.Buffer
			report.Write(&out)
			if !strings.Contains(out.String(), tt.want) {
				t.Errorf("Write() output lacks %q:\n%s", tt.want, out.String())
			}
		})
	}
}
