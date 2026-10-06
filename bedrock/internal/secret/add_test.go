package secret

import (
	"bytes"
	"context"
	"maps"
	"strconv"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"
)

// store is a Secret Manager for adding: the containers it holds, by resource name, with
// their labels and their version count, and what was created and added.
type store struct {
	containers map[string]map[string]string
	versions   map[string]int
	openErr    error
	createErr  error
	created    []string
	added      map[string][]byte
}

func (s *store) open(_ context.Context, _ string) (Client, error) {
	if s.openErr != nil {
		return nil, s.openErr
	}

	return s, nil
}

func (s *store) GetSecretVersion(context.Context, string) (*Version, error) {
	return nil, errors.New("adding asks about no version")
}

func (s *store) ListSecrets(context.Context, string, string) ([]string, error) {
	return nil, errors.New("adding lists no secrets")
}

func (s *store) ListSecretVersions(context.Context, string, string) ([]Version, error) {
	return nil, errors.New("adding lists no versions")
}

func (s *store) GetSecret(_ context.Context, name string) (labels map[string]string, exists bool, err error) {
	labels, exists = s.containers[name]

	return labels, exists, nil
}

func (s *store) CreateSecret(_ context.Context, project, id string, labels map[string]string) error {
	if s.createErr != nil {
		return s.createErr
	}
	name := "projects/" + project + "/secrets/" + id
	if s.containers == nil {
		s.containers = map[string]map[string]string{}
	}
	s.containers[name] = maps.Clone(labels)
	s.created = append(s.created, name)

	return nil
}

func (s *store) AddSecretVersion(_ context.Context, name string, payload []byte) (string, error) {
	if _, ok := s.containers[name]; !ok {
		return "", errors.Newf("rpc error: code = NotFound desc = Secret [%s] not found", name)
	}
	if s.versions == nil {
		s.versions = map[string]int{}
	}
	s.versions[name]++
	if s.added == nil {
		s.added = map[string][]byte{}
	}
	s.added[name] = payload

	return strconv.Itoa(s.versions[name]), nil
}

func TestAdd(t *testing.T) {
	t.Parallel()

	const (
		project   = "lab-tst-1"
		container = "imp-tst-gbl-quill-mail-api-key"
		name      = "projects/" + project + "/secrets/" + container
	)
	labels := ContainerLabels("quill", "tst", "quill", "APP_MAIL_API_KEY", map[string]string{"bedrock-lab": "true"})
	request := func(mutate func(*AddRequest)) *AddRequest {
		req := &AddRequest{App: "quill", Env: "tst", Variable: "APP_MAIL_API_KEY", Project: project, Container: container, Labels: labels, Value: []byte("k-1")}
		if mutate != nil {
			mutate(req)
		}

		return req
	}

	tests := []struct {
		name        string
		store       *store
		req         *AddRequest
		wantErr     string
		wantCreated bool
		wantVersion string
		wantOut     []string
	}{
		{
			name:        "a container the project lacks is created and the value added as version 1",
			store:       &store{},
			req:         request(nil),
			wantCreated: true,
			wantVersion: "1",
			wantOut: []string{
				"Created the container imp-tst-gbl-quill-mail-api-key in project lab-tst-1; the next apply of the quill stack in tst adopts it.",
				"Added version 1 of imp-tst-gbl-quill-mail-api-key in project lab-tst-1, for APP_MAIL_API_KEY of quill in tst.",
				"Pin it: bedrock secret pin tst APP_MAIL_API_KEY 1",
			},
		},
		{
			name:        "a container that exists takes the next version and is not created again",
			store:       &store{containers: map[string]map[string]string{name: labels}, versions: map[string]int{name: 2}},
			req:         request(nil),
			wantVersion: "3",
			wantOut:     []string{"Added version 3 of imp-tst-gbl-quill-mail-api-key in project lab-tst-1", "Pin it: bedrock secret pin tst APP_MAIL_API_KEY 3"},
		},
		{
			name:    "an empty value is refused before anything is asked",
			store:   &store{openErr: errors.New("must not open")},
			req:     request(func(r *AddRequest) { r.Value = nil }),
			wantErr: "the value of APP_MAIL_API_KEY is empty: nothing to add",
		},
		{
			name:    "a missing project is refused",
			store:   &store{openErr: errors.New("must not open")},
			req:     request(func(r *AddRequest) { r.Project = "" }),
			wantErr: "the environment project is needed: pass --project <id>",
		},
		{
			name:    "a missing container name is refused",
			store:   &store{openErr: errors.New("must not open")},
			req:     request(func(r *AddRequest) { r.Container = "" }),
			wantErr: "the container's name is needed: pass --container <name>",
		},
		{
			name:    "a variable of the wrong shape is refused",
			store:   &store{openErr: errors.New("must not open")},
			req:     request(func(r *AddRequest) { r.Variable = "mail_api_key" }),
			wantErr: `variable "mail_api_key": an environment variable in upper snake case under the APP_ prefix`,
		},
		{
			name:    "a client that cannot open is reported",
			store:   &store{openErr: errors.New("no credentials")},
			req:     request(nil),
			wantErr: "no credentials",
		},
		{
			name:    "a refused creation is reported and nothing is added",
			store:   &store{createErr: errors.New("permission denied: secretmanager.secrets.create")},
			req:     request(nil),
			wantErr: "permission denied: secretmanager.secrets.create",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, err := Add(context.Background(), tt.store.open, tt.req)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Add() error = %v, want %q", err, tt.wantErr)
				}
				if len(tt.store.added) != 0 {
					t.Errorf("Add() added %v, want nothing", tt.store.added)
				}

				return
			}
			if err != nil {
				t.Fatalf("Add() error = %v", err)
			}
			if r.Created != tt.wantCreated || r.Version != tt.wantVersion {
				t.Errorf("Add() = created %t version %s, want created %t version %s", r.Created, r.Version, tt.wantCreated, tt.wantVersion)
			}
			if tt.wantCreated && !maps.Equal(tt.store.containers[name], labels) {
				t.Errorf("Add() created with labels %v, want %v", tt.store.containers[name], labels)
			}
			if got := string(tt.store.added[name]); got != "k-1" {
				t.Errorf("Add() stored %q, want %q", got, "k-1")
			}
			var buf bytes.Buffer
			r.Write(&buf)
			for _, want := range tt.wantOut {
				if !strings.Contains(buf.String(), want) {
					t.Errorf("Write() = %q, want it to contain %q", buf.String(), want)
				}
			}
		})
	}
}

func TestContainerLabels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		repository string
		extra      map[string]string
		want       map[string]string
	}{
		{
			name:       "the stack's labels and the variable",
			repository: "quill",
			extra:      map[string]string{"bedrock-lab": "true"},
			want: map[string]string{
				"terraform": "true", "terraform_source_path": "3-app-quill", "source_repo": "quill",
				"environment": "tst", "application": "quill", "variable": "app_mail_api_key", "bedrock-lab": "true",
			},
		},
		{
			name: "without a repository or extra labels",
			want: map[string]string{
				"terraform": "true", "terraform_source_path": "3-app-quill",
				"environment": "tst", "application": "quill", "variable": "app_mail_api_key",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ContainerLabels("quill", "tst", tt.repository, "APP_MAIL_API_KEY", tt.extra); !maps.Equal(got, tt.want) {
				t.Errorf("ContainerLabels() = %v, want %v", got, tt.want)
			}
		})
	}
}
