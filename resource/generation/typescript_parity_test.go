package generation

import (
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
)

// Test_collectionMismatchLines pins the actionable-first rendering of parity
// differences: permission-level groups collapse into one bullet naming the exact
// declaration (folding in their field tags), field-level-only groups are withheld
// behind a count while actionable items remain, and print individually once none do.
func Test_collectionMismatchLines(t *testing.T) {
	t.Parallel()

	locations := map[accesstypes.Resource]declLocation{
		"Contacts": {Name: "Contact", Position: "pkg/resources/contacts.go:12"},
	}
	constants := map[accesstypes.Resource]declLocation{
		"UploadThings": {Name: "UploadThing", Position: "pkg/resources/types.go:6"},
	}

	tests := []struct {
		name         string
		diffs        []resource.CollectionDiff
		wantExact    []string         // exact bullets, in order; sets the expected count
		wantContains map[int][]string // bullet index -> required substrings; keys set the expected count when wantExact is nil
		wantAbsent   []string         // substrings no bullet may contain
		wantFooter   string           // required footer substring
	}{
		{
			name: "hand-written Set registration names the annotation, struct, and scope",
			diffs: []resource.CollectionDiff{{
				Resource:    "Contacts",
				Scope:       accesstypes.DomainPermissionScope,
				RuntimeOnly: true,
				Permissions: []accesstypes.Permission{accesstypes.List, accesstypes.Read},
				Tags:        []accesstypes.Tag{"email", "id", "name"},
			}},
			wantContains: map[int][]string{0: {
				"`// @manualAddResourceSet(listHandler, readHandler)`",
				"the Contact struct at pkg/resources/contacts.go:12",
				"`// @permissionScope(domain)`",
				"3 field tag(s) resolved by the same declaration",
			}},
		},
		{
			name: "manual AddResource provenance names the exact annotation",
			diffs: []resource.CollectionDiff{{
				Resource:    "UploadThings",
				Scope:       accesstypes.GlobalPermissionScope,
				RuntimeOnly: true,
				Permissions: []accesstypes.Permission{accesstypes.Execute},
				ManualRegistrations: []resource.ManualRegistration{
					{Scope: accesstypes.GlobalPermissionScope, Permission: accesstypes.Execute, Resource: "UploadThings"},
				},
			}},
			wantContains: map[int][]string{0: {
				"`// @manualAddResource(Execute)`",
				"the UploadThing constant at pkg/resources/types.go:6",
			}},
		},
		{
			name: "non-Set permission falls back to AddMethodResource guidance",
			diffs: []resource.CollectionDiff{{
				Resource:    "DoThing",
				Scope:       accesstypes.GlobalPermissionScope,
				RuntimeOnly: true,
				Permissions: []accesstypes.Permission{accesstypes.Execute},
			}},
			wantContains: map[int][]string{0: {"Collection.AddMethodResource"}},
		},
		{
			name: "generated-only group points at the declaration",
			diffs: []resource.CollectionDiff{{
				Resource:    "Contacts",
				Scope:       accesstypes.GlobalPermissionScope,
				RuntimeOnly: false,
				Permissions: []accesstypes.Permission{accesstypes.List},
				Tags:        []accesstypes.Tag{"id"},
			}},
			wantContains: map[int][]string{0: {
				"never register at runtime",
				"stale generated code",
				"declared by the Contact struct at pkg/resources/contacts.go:12",
			}},
		},
		{
			name: "field-level groups are withheld while actionable items remain",
			diffs: []resource.CollectionDiff{
				{
					Resource:    "Contacts",
					Scope:       accesstypes.GlobalPermissionScope,
					RuntimeOnly: true,
					Permissions: []accesstypes.Permission{accesstypes.List},
				},
				{
					Resource:    "Widgets",
					Scope:       accesstypes.GlobalPermissionScope,
					RuntimeOnly: true,
					Tags:        []accesstypes.Tag{"secret"},
					TagPermissions: map[accesstypes.Tag][]accesstypes.Permission{
						"name": {accesstypes.Read},
					},
				},
			},
			wantContains: map[int][]string{
				0: {"`// @manualAddResourceSet(listHandler)`"},
				1: {"2 field-level difference(s) on 1 other resource(s) are not shown"},
			},
			wantAbsent: []string{`tag "secret"`},
		},
		{
			name: "field-level groups print individually when nothing is actionable",
			diffs: []resource.CollectionDiff{{
				Resource:       "Widgets",
				Scope:          accesstypes.GlobalPermissionScope,
				RuntimeOnly:    true,
				Tags:           []accesstypes.Tag{"secret"},
				TagPermissions: map[accesstypes.Tag][]accesstypes.Permission{"name": {accesstypes.Read}},
				ImmutableTags:  []accesstypes.Tag{"code"},
			}},
			wantExact: []string{
				`registered at runtime but missing from generated collection: resource "Widgets" scope "global" tag "secret"`,
				`registered at runtime but missing from generated collection: resource "Widgets" scope "global" tag "name" permission "Read"`,
				`registered at runtime but missing from generated collection: resource "Widgets" scope "global" immutable tag "code"`,
			},
			wantFooter: "align the hand-written handler's request struct",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			bullets, footer := collectionMismatchLines(tt.diffs, locations, constants)

			wantCount := len(tt.wantExact)
			if tt.wantExact == nil {
				wantCount = len(tt.wantContains)
			}
			if len(bullets) != wantCount {
				t.Fatalf("collectionMismatchLines() returned %d bullets, want %d: %v", len(bullets), wantCount, bullets)
			}
			for i, want := range tt.wantExact {
				if bullets[i] != want {
					t.Errorf("bullets[%d] = %q, want %q", i, bullets[i], want)
				}
			}
			for i, wants := range tt.wantContains {
				for _, want := range wants {
					if !strings.Contains(bullets[i], want) {
						t.Errorf("bullets[%d] missing %q:\n%s", i, want, bullets[i])
					}
				}
			}
			for _, bullet := range bullets {
				for _, absent := range tt.wantAbsent {
					if strings.Contains(bullet, absent) {
						t.Errorf("bullet must not contain %q:\n%s", absent, bullet)
					}
				}
			}
			if tt.wantFooter != "" && !strings.Contains(footer, tt.wantFooter) {
				t.Errorf("footer missing %q:\n%s", tt.wantFooter, footer)
			}
		})
	}
}

// Test_manualSetHandlerArgs pins the permission-to-handler-type mapping the
// @manualAddResourceSet suggestion is built from.
func Test_manualSetHandlerArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		perms []accesstypes.Permission
		want  []string
		ok    bool
	}{
		{name: "list and read", perms: []accesstypes.Permission{accesstypes.List, accesstypes.Read}, want: []string{"listHandler", "readHandler"}, ok: true},
		{name: "mutating permissions map to patchHandler", perms: []accesstypes.Permission{accesstypes.Create, accesstypes.Update, accesstypes.Delete}, want: []string{"patchHandler"}, ok: true},
		{name: "execute is not Set-shaped", perms: []accesstypes.Permission{accesstypes.Execute}, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := manualSetHandlerArgs(tt.perms)
			if ok != tt.ok {
				t.Fatalf("manualSetHandlerArgs() ok = %v, want %v", ok, tt.ok)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("manualSetHandlerArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}
