package where

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/secret"
)

// fakeProjects answers every search with the same projects and records the queries.
type fakeProjects struct {
	projects []Project
	openErr  error
	err      error
	queries  []string
}

// open is the fake's ProjectClientFunc.
func (f *fakeProjects) open(context.Context) (ProjectClient, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}

	return f, nil
}

func (f *fakeProjects) SearchProjects(_ context.Context, query string) ([]Project, error) {
	f.queries = append(f.queries, query)
	if f.err != nil {
		return nil, f.err
	}

	return f.projects, nil
}

// lab is a project of the lab organization in the environment, labeled as 1-org labels
// the projects it creates.
func lab(id, env string) Project {
	return Project{ID: id, DisplayName: "Lab " + env, State: Active, Labels: map[string]string{"environment": env, "terraform_source_path": "1-org"}}
}

func TestProjectByLabels(t *testing.T) {
	t.Parallel()

	labels := map[string]string{"terraform_source_path": "1-org", "environment": "tst"}
	deleted := lab("lab-tst-0", "tst")
	deleted.State = "DELETE_REQUESTED"
	loose := lab("lab-tst2-1", "tst2")
	tests := []struct {
		name     string
		projects []Project
		openErr  error
		err      error
		want     string
		wantErr  string
	}{
		{name: "the one active project", projects: []Project{lab("lab-tst-1", "tst")}, want: "lab-tst-1"},
		{name: "a project being deleted does not count", projects: []Project{deleted, lab("lab-tst-1", "tst")}, want: "lab-tst-1"},
		{name: "a looser match by the API does not count", projects: []Project{loose, lab("lab-tst-1", "tst")}, want: "lab-tst-1"},
		{name: "none", wantErr: "no active project carries the labels environment=tst, terraform_source_path=1-org: pass --project"},
		{name: "only a project being deleted", projects: []Project{deleted}, wantErr: "no active project carries the labels"},
		{name: "several", projects: []Project{lab("lab-tst-1", "tst"), lab("lab-tst-2", "tst")}, wantErr: "several active projects carry the labels environment=tst, terraform_source_path=1-org: lab-tst-1 (Lab tst), lab-tst-2 (Lab tst); pass --project"},
		{name: "a client that cannot be opened", openErr: errors.New("no credentials"), wantErr: "no credentials"},
		{name: "a search that fails", err: errors.New("permission denied"), wantErr: "permission denied"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := &fakeProjects{projects: tt.projects, openErr: tt.openErr, err: tt.err}
			got, err := ProjectByLabels(context.Background(), f.open, labels)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ProjectByLabels() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("ProjectByLabels() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("ProjectByLabels() = %q, want %q", got, tt.want)
			}
			if want := []string{"labels.environment=tst AND labels.terraform_source_path=1-org"}; !slices.Equal(f.queries, want) {
				t.Errorf("the search was asked %q, want %q", f.queries, want)
			}
		})
	}
}

// fakeSecrets holds, by container name, the versions of each in one project, and
// records the filters it was asked with.
type fakeSecrets struct {
	containers map[string][]secret.Version
	openErr    error
	filters    []string
}

// open is the fake's secret.ClientFunc.
func (f *fakeSecrets) open(context.Context, string) (secret.Client, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}

	return f, nil
}

func (f *fakeSecrets) GetSecretVersion(_ context.Context, name string) (*secret.Version, error) {
	return nil, errors.Newf("not asked about %s", name)
}

func (f *fakeSecrets) ListSecrets(_ context.Context, _, filter string) ([]string, error) {
	f.filters = append(f.filters, filter)
	names := make([]string, 0, len(f.containers))
	for name := range f.containers {
		names = append(names, name)
	}
	slices.Sort(names)

	return names, nil
}

func (f *fakeSecrets) ListSecretVersions(_ context.Context, _, container string) ([]secret.Version, error) {
	versions, ok := f.containers[container]
	if !ok {
		return nil, errors.Newf("no container %s", container)
	}

	return versions, nil
}

// versions makes the container's versions, numbered from 1, in the states given.
func versions(container string, states ...string) []secret.Version {
	out := make([]secret.Version, 0, len(states))
	for i, state := range states {
		out = append(out, secret.Version{Name: "projects/lab-tst-1/secrets/" + container + "/versions/" + strconv.Itoa(i+1), State: state})
	}

	return out
}

func TestContainerByLabels(t *testing.T) {
	t.Parallel()

	labels := map[string]string{"terraform_source_path": "3-app-quill"}
	tests := []struct {
		name       string
		containers []string
		openErr    error
		want       string
		wantErr    string
	}{
		{name: "the one container ending with the suffix", containers: []string{"imp-tst-gbl-quill-cookie-key", "imp-tst-gbl-quill-mail-api-key"}, want: "imp-tst-gbl-quill-cookie-key"},
		{name: "none among the layer's", containers: []string{"imp-tst-gbl-quill-mail-api-key"}, wantErr: "no secret container in project lab-tst-1 carrying the labels terraform_source_path=3-app-quill ends with -cookie-key (found imp-tst-gbl-quill-mail-api-key): pass --container"},
		{name: "no container carries the labels", wantErr: "no secret container in project lab-tst-1 carries the labels terraform_source_path=3-app-quill: pass --container"},
		{name: "several", containers: []string{"imp-tst-gbl-quill-cookie-key", "imp-tst-gbl-quill-old-cookie-key"}, wantErr: "several secret containers in project lab-tst-1 carrying the labels terraform_source_path=3-app-quill end with -cookie-key: imp-tst-gbl-quill-cookie-key, imp-tst-gbl-quill-old-cookie-key; pass --container"},
		{name: "a client that cannot be opened", openErr: errors.New("no credentials"), wantErr: "no credentials"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := &fakeSecrets{containers: map[string][]secret.Version{}, openErr: tt.openErr}
			for _, name := range tt.containers {
				f.containers[name] = versions(name, secret.Enabled)
			}
			got, err := ContainerByLabels(context.Background(), f.open, "lab-tst-1", labels, secret.ContainerSuffix("APP_COOKIE_KEY"))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ContainerByLabels() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("ContainerByLabels() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("ContainerByLabels() = %q, want %q", got, tt.want)
			}
			if want := []string{"labels.terraform_source_path=3-app-quill"}; !slices.Equal(f.filters, want) {
				t.Errorf("the list was filtered by %q, want %q", f.filters, want)
			}
		})
	}
}

func TestVersions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		container string
		states    []string
		openErr   error
		want      []VersionInfo
		wantErr   string
	}{
		{
			name:      "newest first with its state",
			container: "imp-tst-gbl-quill-cookie-key",
			states:    []string{"DESTROYED", "DISABLED", secret.Enabled},
			want:      []VersionInfo{{Name: "3", State: secret.Enabled}, {Name: "2", State: "DISABLED"}, {Name: "1", State: "DESTROYED"}},
		},
		{name: "no versions yet", container: "imp-tst-gbl-quill-cookie-key", want: []VersionInfo{}},
		{name: "a container that is not there", container: "none", states: []string{secret.Enabled}, wantErr: "no container none"},
		{name: "a client that cannot be opened", container: "imp-tst-gbl-quill-cookie-key", openErr: errors.New("no credentials"), wantErr: "no credentials"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := &fakeSecrets{containers: map[string][]secret.Version{"imp-tst-gbl-quill-cookie-key": versions("imp-tst-gbl-quill-cookie-key", tt.states...)}, openErr: tt.openErr}
			got, err := Versions(context.Background(), f.open, "lab-tst-1", tt.container)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Versions() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Versions() error = %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Versions() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLabelQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{name: "one label", labels: map[string]string{"terraform_source_path": "3-app-quill"}, want: "labels.terraform_source_path=3-app-quill"},
		{name: "two labels, keys sorted", labels: map[string]string{"terraform_source_path": "1-org", "environment": "tst"}, want: "labels.environment=tst AND labels.terraform_source_path=1-org"},
		{name: "no labels", labels: map[string]string{}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := labelQuery(tt.labels); got != tt.want {
				t.Errorf("labelQuery() = %q, want %q", got, tt.want)
			}
		})
	}
}
