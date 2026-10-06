package org

import (
	"context"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
)

// fakePolicies answers each project's IAM policy from a map; a project it lacks is an
// error, as the API answers for one the caller cannot read.
type fakePolicies struct {
	byProject map[string][]Binding
}

func (f *fakePolicies) ProjectPolicy(_ context.Context, project string) ([]Binding, error) {
	bindings, ok := f.byProject[project]
	if !ok {
		return nil, errors.Newf("GetIamPolicy(): %s: permission denied", project)
	}

	return bindings, nil
}

func (*fakePolicies) Close() error {
	return nil
}

// TestOwners: the people holding roles/owner on the environment projects the placement
// records are listed in promotion order; service accounts and groups on the role, and
// people on other roles, are not; an environment without a project is named, not read.
func TestOwners(t *testing.T) {
	t.Parallel()

	policies := map[string][]Binding{
		"imp-tst-gbl-core-1a2b": {
			{Role: "roles/owner", Members: []string{"user:seed@imp.example", "serviceAccount:imp-tst-gbl-tofu@imp-tst-gbl-core-1a2b.iam.gserviceaccount.com"}},
			{Role: "roles/editor", Members: []string{"user:someone@imp.example"}},
		},
		"imp-stg-gbl-core-3c4d": {
			{Role: "roles/owner", Members: []string{"group:team-stg@imp.example"}},
		},
		"imp-prd-gbl-core-5e6f": {
			{Role: "roles/owner", Members: []string{"user:seed@imp.example", "user:other@imp.example"}},
		},
	}
	tests := []struct {
		name           string
		projects       map[string]string
		wantOwners     []Owner
		wantUnrecorded string
		wantErr        string
	}{
		{
			name:     "every project recorded: the people on roles/owner, in promotion order",
			projects: map[string]string{"tst": "imp-tst-gbl-core-1a2b", "stg": "imp-stg-gbl-core-3c4d", "prd": "imp-prd-gbl-core-5e6f"},
			wantOwners: []Owner{
				{Environment: "tst", Project: "imp-tst-gbl-core-1a2b", Member: "user:seed@imp.example"},
				{Environment: "prd", Project: "imp-prd-gbl-core-5e6f", Member: "user:seed@imp.example"},
				{Environment: "prd", Project: "imp-prd-gbl-core-5e6f", Member: "user:other@imp.example"},
			},
		},
		{
			name:           "an environment without a project is named and not read",
			projects:       map[string]string{"stg": "imp-stg-gbl-core-3c4d"},
			wantUnrecorded: "tst,prd",
		},
		{
			name:     "a policy that cannot be read fails the listing",
			projects: map[string]string{"tst": "imp-qa-gbl-core-0000"},
			wantErr:  "GetIamPolicy(): imp-qa-gbl-core-0000: permission denied",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &Placement{Projects: tt.projects}
			owners, unrecorded, err := Owners(context.Background(), p, &fakePolicies{byProject: policies})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Owners() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Owners() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantOwners, owners); diff != "" {
				t.Errorf("owners mismatch (-want +got):\n%s", diff)
			}
			if got := strings.Join(unrecorded, ","); got != tt.wantUnrecorded {
				t.Errorf("unrecorded = %q, want %q", got, tt.wantUnrecorded)
			}
		})
	}
}
