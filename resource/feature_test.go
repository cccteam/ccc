package resource

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

func TestValidateFeatureDeclarations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		declared []FeatureDeclaration
		wantErr  string
	}{
		{
			name:     "names in the shape, each once",
			declared: []FeatureDeclaration{{Name: "debriefs", Constant: "Debriefs"}, {Name: "cargo_manifest2", Constant: "CargoManifest"}},
		},
		{
			name:     "an uppercase letter is refused naming the constant",
			declared: []FeatureDeclaration{{Name: "Debriefs", Constant: "Debriefs"}},
			wantErr:  `feature flag constant Debriefs: "Debriefs" is not a feature name`,
		},
		{
			name:     "a leading digit is refused",
			declared: []FeatureDeclaration{{Name: "2debriefs", Constant: "Debriefs"}},
			wantErr:  `"2debriefs" is not a feature name`,
		},
		{
			name:     "a hyphen is refused",
			declared: []FeatureDeclaration{{Name: "cargo-manifest", Constant: "CargoManifest"}},
			wantErr:  `"cargo-manifest" is not a feature name`,
		},
		{
			name:     "a name over 64 characters is refused",
			declared: []FeatureDeclaration{{Name: Feature("a" + strings.Repeat("b", 64)), Constant: "Long"}},
			wantErr:  "is not a feature name",
		},
		{
			name:     "two constants declaring one name are refused naming both",
			declared: []FeatureDeclaration{{Name: "debriefs", Constant: "Debriefs"}, {Name: "debriefs", Constant: "DebriefsAgain"}},
			wantErr:  `feature flag constant DebriefsAgain declares "debriefs", which constant Debriefs already declares`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateFeatureDeclarations(tt.declared)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateFeatureDeclarations() error = %v", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateFeatureDeclarations() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// TestFeatureFlagsDDL pins the two tables' statements per database, and pins the test
// fixture migration to the Spanner statements, so the fixture the emulator tests run on
// is what an application copies into its own migration.
func TestFeatureFlagsDDL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		dbType       DBType
		wantCount    int
		wantContains []string
	}{
		{
			name:      "Spanner: both tables, commit timestamps on the write columns",
			dbType:    SpannerDBType,
			wantCount: 2,
			wantContains: []string{
				"CREATE TABLE FeatureFlags (",
				"Name STRING(64) NOT NULL,",
				"UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),",
				") PRIMARY KEY (Name)",
				"CREATE TABLE FeatureFlagChanges (",
				"ChangedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),",
				") PRIMARY KEY (Name, ChangedAt)",
			},
		},
		{
			name:      "Postgres: both tables, quoted identifiers",
			dbType:    PostgresDBType,
			wantCount: 2,
			wantContains: []string{
				`CREATE TABLE "FeatureFlags" (`,
				`"Name" VARCHAR(64) NOT NULL,`,
				`"UpdatedAt" TIMESTAMPTZ NOT NULL,`,
				`PRIMARY KEY ("Name")`,
				`CREATE TABLE "FeatureFlagChanges" (`,
				`PRIMARY KEY ("Name", "ChangedAt")`,
			},
		},
		{name: "an unknown database has no statements", dbType: DBType("other")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := FeatureFlagsDDL(tt.dbType)
			if len(got) != tt.wantCount {
				t.Fatalf("FeatureFlagsDDL(%s) = %d statements, want %d", tt.dbType, len(got), tt.wantCount)
			}
			joined := strings.Join(got, "\n")
			for _, want := range tt.wantContains {
				if !strings.Contains(joined, want) {
					t.Errorf("FeatureFlagsDDL(%s) missing %q:\n%s", tt.dbType, want, joined)
				}
			}
		})
	}

	t.Run("the fixture migration is the Spanner statements", func(t *testing.T) {
		t.Parallel()

		fixture, err := os.ReadFile("testdata/features/schema/000001_feature_flags.up.sql")
		if err != nil {
			t.Fatalf("os.ReadFile() error = %v", err)
		}
		for _, stmt := range FeatureFlagsDDL(SpannerDBType) {
			if !strings.Contains(string(fixture), stmt+";") {
				t.Errorf("the fixture migration does not carry the statement:\n%s", stmt)
			}
		}
	})
}

// featureSetOf builds a FeatureSet holding the given flags, for the tests that need no
// database behind it.
func featureSetOf(flags map[Feature]bool) *FeatureSet {
	s := &FeatureSet{flags: make(map[Feature]FeatureFlag, len(flags))}
	for name, enabled := range flags {
		s.flags[name] = FeatureFlag{Name: string(name), Enabled: enabled}
	}

	return s
}

func TestFeatureSet_nil(t *testing.T) {
	t.Parallel()

	var s *FeatureSet
	if s.Enabled("debriefs") {
		t.Error("a nil FeatureSet answers a flag on; every flag of a nil set is off")
	}
	if _, ok := s.Flag("debriefs"); ok {
		t.Error("a nil FeatureSet holds a flag")
	}
	if got := s.EnabledNames(); got == nil || len(got) != 0 {
		t.Errorf("EnabledNames() = %v, want an empty, non-nil list", got)
	}
	if err := s.Reload(t.Context()); err == nil {
		t.Error("Reload() on a nil FeatureSet error = nil, want an error")
	}
	if err := s.Follow(t.Context(), nil); err == nil {
		t.Error("Follow() on a nil FeatureSet error = nil, want an error")
	}
}

func TestFeatureGates_Off(t *testing.T) {
	t.Parallel()

	gates := FeatureGates{
		"Debriefs":        "debriefs",
		"Ships.cargoBays": "cargo_manifest",
		"SetDebrief":      "debriefs",
	}
	features := featureSetOf(map[Feature]bool{"debriefs": false, "cargo_manifest": true})

	tests := []struct {
		name string
		key  accesstypes.Resource
		want bool
	}{
		{name: "a gated resource whose flag is off", key: "Debriefs", want: true},
		{name: "a field of a gated-off resource goes with it", key: "Debriefs.summary", want: true},
		{name: "a gated method whose flag is off", key: "SetDebrief", want: true},
		{name: "a gated field whose own flag is on", key: "Ships.cargoBays", want: false},
		{name: "an ungated field of an ungated resource", key: "Ships.name", want: false},
		{name: "an ungated resource", key: "Ships", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := gates.Off(features, tt.key); got != tt.want {
				t.Errorf("Off(%s) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestFeatureGuard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		features   *FeatureSet
		wantStatus int
		wantBody   string
		wantNext   bool
	}{
		{name: "no FeatureSet: every gate is closed", wantStatus: http.StatusNotFound, wantBody: "Not Found\n"},
		{name: "the flag off answers as the outlet's not-found handler", features: featureSetOf(map[Feature]bool{"debriefs": false}), wantStatus: http.StatusNotFound, wantBody: "Not Found\n"},
		{name: "a flag the table does not hold is off", features: featureSetOf(nil), wantStatus: http.StatusNotFound, wantBody: "Not Found\n"},
		{name: "the flag on passes the request through", features: featureSetOf(map[Feature]bool{"debriefs": true}), wantStatus: http.StatusOK, wantBody: "served", wantNext: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			called := false
			next := func(w http.ResponseWriter, _ *http.Request) {
				called = true
				_, _ = w.Write([]byte("served"))
			}
			rr := httptest.NewRecorder()
			FeatureGuard(tt.features, "debriefs")(next).ServeHTTP(rr, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/debriefs", http.NoBody))

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tt.wantStatus)
			}
			if rr.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rr.Body.String(), tt.wantBody)
			}
			if called != tt.wantNext {
				t.Errorf("next called = %v, want %v", called, tt.wantNext)
			}
		})
	}
}

func TestFeaturesHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		features *FeatureSet
		wantBody string
	}{
		{name: "no FeatureSet: nothing is on, and the list is empty, never null", wantBody: `{"enabled":[]}`},
		{name: "the flags that are on, sorted", features: featureSetOf(map[Feature]bool{"debriefs": true, "cargo_manifest": true, "beacons": false}), wantBody: `{"enabled":["cargo_manifest","debriefs"]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rr := httptest.NewRecorder()
			FeaturesHandler(tt.features).ServeHTTP(rr, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/features", http.NoBody))

			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %q)", rr.Code, rr.Body.String())
			}
			if got := strings.TrimSpace(rr.Body.String()); got != tt.wantBody {
				t.Errorf("body = %s, want %s", got, tt.wantBody)
			}
		})
	}
}

// TestPermissionDigestHandler_featureGates pins the digest filter: an entry behind a
// flag that is off is left out, a resource's fields with it, and everything else stays.
func TestPermissionDigestHandler_featureGates(t *testing.T) {
	t.Parallel()

	digest := func() accesstypes.PermissionDigest {
		return accesstypes.PermissionDigest{
			"Ships":            {"List": accesstypes.DigestGranted},
			"Ships.name":       {"List": accesstypes.DigestGranted},
			"Ships.cargoBays":  {"List": accesstypes.DigestGranted},
			"Debriefs":         {"List": accesstypes.DigestGranted},
			"Debriefs.summary": {"List": accesstypes.DigestGranted},
			"SetDebrief":       {"Execute": accesstypes.DigestGranted},
		}
	}
	gates := FeatureGates{"Debriefs": "debriefs", "Ships.cargoBays": "cargo_manifest", "SetDebrief": "debriefs"}

	tests := []struct {
		name     string
		opts     []DigestOption
		wantKeys []string
	}{
		{
			name:     "no gates: the digest as the engine answered it",
			wantKeys: []string{"Debriefs", "Debriefs.summary", "SetDebrief", "Ships", "Ships.cargoBays", "Ships.name"},
		},
		{
			name:     "every flag off: the gated resource, its field, the gated field and the gated method are left out",
			opts:     []DigestOption{WithFeatureGates(gates, nil)},
			wantKeys: []string{"Ships", "Ships.name"},
		},
		{
			name:     "one flag on: its entries return",
			opts:     []DigestOption{WithFeatureGates(gates, featureSetOf(map[Feature]bool{"debriefs": true}))},
			wantKeys: []string{"Debriefs", "Debriefs.summary", "SetDebrief", "Ships", "Ships.name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stub := &digestStubPermissions{digest: digest()}
			handler := PermissionDigestHandler(func(*http.Request) UserPermissions { return stub }, tt.opts...)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/permission-digest", http.NoBody))
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %q)", rr.Code, rr.Body.String())
			}

			var got accesstypes.PermissionDigest
			if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			keys := make([]string, 0, len(got))
			for key := range got {
				keys = append(keys, string(key))
			}
			slices.Sort(keys)
			if diff := cmp.Diff(tt.wantKeys, keys); diff != "" {
				t.Errorf("digest keys mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// gatedResource and gatedRequest are a resource with one field behind a flag, as the
// generator declares them: the feature tag on the request struct's field.
type gatedResource struct {
	ID     string `spanner:"Id"`
	Name   string `spanner:"Name"`
	Secret string `spanner:"Secret"`
}

func (gatedResource) Resource() accesstypes.Resource {
	return "GatedRows"
}

type gatedRequest struct {
	ID     string `json:"id"     index:"true"      perm:"-"`
	Name   string `json:"name"   index:"true"`
	Secret string `json:"secret" feature:"secrets" index:"true"`
}

// TestQueryDecoder_gatedFields pins the decoders' answer for a field behind a flag: off,
// the field is unknown to columns, sort and filter and absent from the default
// projection; on, it is an ordinary field.
func TestQueryDecoder_gatedFields(t *testing.T) {
	t.Parallel()

	off := featureSetOf(map[Feature]bool{"secrets": false})
	on := featureSetOf(map[Feature]bool{"secrets": true})

	tests := []struct {
		name            string
		features        *FeatureSet
		target          string
		wantErr         string
		wantRequestable []accesstypes.Field
		wantFields      []accesstypes.Field
	}{
		{name: "off: the default projection leaves the field out", features: off, target: "/gated?sort=id", wantRequestable: []accesstypes.Field{"ID", "Name"}},
		{name: "no FeatureSet: the field is hidden", target: "/gated?sort=id", wantRequestable: []accesstypes.Field{"ID", "Name"}},
		{name: "off: asking for the column is an unknown column", features: off, target: "/gated?sort=id&columns=id,secret", wantErr: "unknown column: secret"},
		{name: "off: sorting by the field is an unknown sort field", features: off, target: "/gated?sort=secret", wantErr: "unknown sort field: secret"},
		{name: "off: filtering by the field is refused", features: off, target: "/gated?sort=id&filter=secret:eq:x", wantErr: "secret"},
		{name: "on: the default projection carries the field", features: on, target: "/gated?sort=id", wantRequestable: []accesstypes.Field{"ID", "Name", "Secret"}},
		{name: "on: the column may be asked for", features: on, target: "/gated?sort=id&columns=id,secret", wantRequestable: []accesstypes.Field{"ID", "Name", "Secret"}, wantFields: []accesstypes.Field{"ID", "Secret"}},
		{name: "on: the field sorts", features: on, target: "/gated?sort=secret", wantRequestable: []accesstypes.Field{"ID", "Name", "Secret"}},
		{name: "on: the field filters", features: on, target: "/gated?sort=id&filter=secret:eq:x", wantRequestable: []accesstypes.Field{"ID", "Name", "Secret"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rSet, err := NewSet[gatedResource, gatedRequest](accesstypes.List)
			if err != nil {
				t.Fatalf("NewSet() error = %v", err)
			}
			decoder, err := NewQueryDecoder[gatedResource, gatedRequest](rSet)
			if err != nil {
				t.Fatalf("NewQueryDecoder() error = %v", err)
			}
			decoder.WithFeatures(tt.features)

			qSet, err := decoder.DecodeWithoutPermissions(httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.target, http.NoBody))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("DecodeWithoutPermissions() error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("DecodeWithoutPermissions() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantRequestable, qSet.requestableFields); diff != "" {
				t.Errorf("requestable fields mismatch (-want +got):\n%s", diff)
			}
			if tt.wantFields != nil {
				if diff := cmp.Diff(tt.wantFields, qSet.Fields()); diff != "" {
					t.Errorf("Fields() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// TestDecoder_gatedFields pins the patch decoder's answer: a body naming a field behind
// a flag that is off is refused as a body naming an undeclared field is.
func TestDecoder_gatedFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		features *FeatureSet
		body     string
		wantErr  string
	}{
		{name: "off: the field is invalid in the body", features: featureSetOf(map[Feature]bool{"secrets": false}), body: `{"name":"n","secret":"s"}`, wantErr: "invalid field in json - secret"},
		{name: "no FeatureSet: the field is invalid in the body", body: `{"secret":"s"}`, wantErr: "invalid field in json - secret"},
		{name: "off: a body without the field decodes", features: featureSetOf(map[Feature]bool{"secrets": false}), body: `{"name":"n"}`},
		{name: "on: the field decodes", features: featureSetOf(map[Feature]bool{"secrets": true}), body: `{"name":"n","secret":"s"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rSet, err := NewSet[gatedResource, gatedRequest](accesstypes.Create)
			if err != nil {
				t.Fatalf("NewSet() error = %v", err)
			}
			decoder, err := NewDecoder[gatedResource, gatedRequest](rSet)
			if err != nil {
				t.Fatalf("NewDecoder() error = %v", err)
			}
			decoder = decoder.WithFeatures(tt.features)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/gated", strings.NewReader(tt.body))
			_, err = decoder.DecodeWithoutPermissions(req)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("DecodeWithoutPermissions() error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("DecodeWithoutPermissions() error = %v", err)
			}
		})
	}
}

// TestNewSet_featureTag pins the stale-struct guard on the feature tag: a value that is
// not a flag name fails at construction, as a stale perm value does.
func TestNewSet_featureTag(t *testing.T) {
	t.Parallel()

	type badRequest struct {
		ID     string `json:"id"     perm:"-"`
		Secret string `json:"secret" feature:"Not-A-Flag"`
	}

	if _, err := NewSet[gatedResource, badRequest](accesstypes.List); err == nil || !strings.Contains(err.Error(), `feature:"Not-A-Flag" on field Secret is not a feature name`) {
		t.Fatalf("NewSet() error = %v, want the feature tag refused", err)
	}
	rSet, err := NewSet[gatedResource, gatedRequest](accesstypes.List)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	if diff := cmp.Diff(map[accesstypes.Field]Feature{"Secret": "secrets"}, rSet.gatedFields); diff != "" {
		t.Errorf("gated fields mismatch (-want +got):\n%s", diff)
	}
}

// TestFeatureFlagFieldTags pins the fields a FeatureFlags grant may name: the five
// columns by wire name, the name exempt as the key.
func TestFeatureFlagFieldTags(t *testing.T) {
	t.Parallel()

	set, err := NewSetData(FeatureFlagFieldTags(), accesstypes.List)
	if err != nil {
		t.Fatalf("NewSetData() error = %v", err)
	}
	want := accesstypes.TagPermissions{
		"name":        {accesstypes.NullPermission},
		"description": {accesstypes.List},
		"enabled":     {accesstypes.List},
		"updatedAt":   {accesstypes.List},
		"updatedBy":   {accesstypes.List},
	}
	if diff := cmp.Diff(want, set.TagPermissions); diff != "" {
		t.Errorf("tag permissions mismatch (-want +got):\n%s", diff)
	}
	order, keys := FeatureFlagsQueryKeys()
	if diff := cmp.Diff([]accesstypes.Tag{"name"}, order); diff != "" {
		t.Errorf("order mismatch (-want +got):\n%s", diff)
	}
	if len(keys) != 0 {
		t.Errorf("query keys = %v, want none beyond the key", keys)
	}
}
