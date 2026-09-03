package resource

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

// capStubPermissions answers Check per permission: a permission listed in
// byPerm uses its decision table with absence reading Denied (the zero
// Decision), any other permission answers all-Granted so the read gate stays
// out of the way.
type capStubPermissions struct {
	byPerm map[accesstypes.Permission]accesstypes.Decisions
}

func (s capStubPermissions) Check(_ context.Context, _ accesstypes.Environment, _ accesstypes.Scope, perm accesstypes.Permission, resources ...accesstypes.Resource) (accesstypes.Decisions, error) {
	out := make(accesstypes.Decisions, len(resources))
	table, scripted := s.byPerm[perm]
	for _, res := range resources {
		if scripted {
			out[res] = table[res]
		} else {
			out[res] = accesstypes.Granted()
		}
	}

	return out, nil
}

func (capStubPermissions) PermissionDigest(context.Context, accesstypes.Scope) (accesstypes.PermissionDigest, error) {
	return accesstypes.PermissionDigest{}, nil
}

func (capStubPermissions) Domains(context.Context) ([]accesstypes.Domain, error) {
	return []accesstypes.Domain{}, nil
}

func (capStubPermissions) User() accesstypes.User { return "u1" }

// TestQuerySet_stmt_capabilities pins the capability envelope's statement
// contract (§13): a capability-free request renders byte-identically, pure
// RBAC adds no SQL (answers assemble from grants alone), conditional grants
// render as one deduplicated boolean group in the reserved array column, and
// a new.-referencing condition counts potentially-true with no SQL.
func TestQuerySet_stmt_capabilities(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	baseSQL := "SELECT Id, Public, Tagged FROM enforcementResources WHERE (`enforcementResources`.`Station` = @domain)"

	tests := []struct {
		name    string
		request []accesstypes.Permission
		byPerm  map[accesstypes.Permission]accesstypes.Decisions
		wantSQL string
		// wantAssembled maps a checks vector (keyed by name) to the expected
		// per-row property; nil checks exercises the data-free path.
		checks   []bool
		want     map[accesstypes.Permission]any
		wantPlan bool
	}{
		{
			name:    "capability-free request renders the plain statement",
			request: nil,
			wantSQL: baseSQL,
		},
		{
			name:    "pure RBAC adds no SQL and assembles from grants alone",
			request: []accesstypes.Permission{accesstypes.Update, accesstypes.Delete},
			byPerm: map[accesstypes.Permission]accesstypes.Decisions{
				accesstypes.Update: {
					enforcedResource + ".public": accesstypes.Granted(),
					enforcedResource + ".tagged": accesstypes.Granted(),
				},
				accesstypes.Delete: {enforcedResource: accesstypes.Granted()},
			},
			wantSQL:  baseSQL,
			want:     map[accesstypes.Permission]any{accesstypes.Update: []string{"public", "tagged"}, accesstypes.Delete: true},
			wantPlan: true,
		},
		{
			name:    "denied grants drop the field and kill the delete",
			request: []accesstypes.Permission{accesstypes.Update, accesstypes.Delete},
			byPerm: map[accesstypes.Permission]accesstypes.Decisions{
				accesstypes.Update: {enforcedResource + ".tagged": accesstypes.Granted()},
				accesstypes.Delete: {},
			},
			wantSQL:  baseSQL,
			want:     map[accesstypes.Permission]any{accesstypes.Update: []string{"tagged"}, accesstypes.Delete: false},
			wantPlan: true,
		},
		{
			name:    "one shared condition renders one boolean group across fields and permissions",
			request: []accesstypes.Permission{accesstypes.Update, accesstypes.Delete},
			byPerm: map[accesstypes.Permission]accesstypes.Decisions{
				accesstypes.Update: {
					enforcedResource + ".public": conditionalOn(enforcedResource+".public", "owner = subject"),
					enforcedResource + ".tagged": conditionalOn(enforcedResource+".tagged", "owner = subject"),
				},
				accesstypes.Delete: {enforcedResource: conditionalOn(enforcedResource, "owner = subject")},
			},
			wantSQL: "SELECT Id, Public, Tagged, ARRAY<BOOL>[(`enforcementResources`.`Owner` = @subject)] AS zzCapabilityChecks " +
				"FROM enforcementResources WHERE (`enforcementResources`.`Station` = @domain)",
			checks:   []bool{true},
			want:     map[accesstypes.Permission]any{accesstypes.Update: []string{"public", "tagged"}, accesstypes.Delete: true},
			wantPlan: true,
		},
		{
			name:    "a post-image condition counts potentially-true with no SQL",
			request: []accesstypes.Permission{accesstypes.Update},
			byPerm: map[accesstypes.Permission]accesstypes.Decisions{
				accesstypes.Update: {
					enforcedResource + ".public": accesstypes.Granted(),
					enforcedResource + ".tagged": conditionalOn(enforcedResource+".tagged", "new.priority <= 3"),
				},
			},
			wantSQL:  baseSQL,
			want:     map[accesstypes.Permission]any{accesstypes.Update: []string{"public", "tagged"}},
			wantPlan: true,
		},
		{
			// The fail-open bound is per TERM (§13, WithoutPostImage): the
			// old-vs-new conjunct assumes TRUE while the guard beside it
			// still renders and narrows per row — the grant does not collapse
			// to always-editable.
			name:    "a post-image conjunct keeps its evaluable residue",
			request: []accesstypes.Permission{accesstypes.Update},
			byPerm: map[accesstypes.Permission]accesstypes.Decisions{
				accesstypes.Update: {
					enforcedResource + ".tagged": conditionalOn(enforcedResource+".tagged", "owner = subject AND new.priority <= priority"),
				},
			},
			wantSQL: "SELECT Id, Public, Tagged, ARRAY<BOOL>[(`enforcementResources`.`Owner` = @subject)] AS zzCapabilityChecks " +
				"FROM enforcementResources WHERE (`enforcementResources`.`Station` = @domain)",
			checks:   []bool{false},
			want:     map[accesstypes.Permission]any{accesstypes.Update: []string{}},
			wantPlan: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rSet, err := NewSet[enforcementResource, enforcementReadRequest](accesstypes.Read)
			if err != nil {
				t.Fatalf("NewSet() error = %v", err)
			}

			q := NewQuerySet(NewMetadata[enforcementResource]())
			q.env = accesstypes.EnvironmentAt(now)
			q.jsonNames = map[accesstypes.Field]string{"ID": "id", "Public": "public", "Tagged": "tagged"}
			q.collection = renderCollection(t)
			q.EnableUserPermissionEnforcement(rSet, capStubPermissions{byPerm: tt.byPerm}, testScope, accesstypes.Read)
			for _, field := range []accesstypes.Field{"ID", "Public", "Tagged"} {
				q.AddField(field)
			}
			q.RequestCapabilities(tt.request...)

			if err := q.checkPermissions(t.Context(), SpannerDBType); err != nil {
				t.Fatalf("QuerySet.checkPermissions() error = %v", err)
			}

			stmt, err := q.stmt(SpannerDBType)
			if err != nil {
				t.Fatalf("QuerySet.stmt() error = %v", err)
			}

			if got := normalizeSQL(stmt.SQL); got != tt.wantSQL {
				t.Errorf("QuerySet.stmt() SQL =\n%s\nwant\n%s", got, tt.wantSQL)
			}

			if !tt.wantPlan {
				if stmt.capabilityPlan != nil {
					t.Fatalf("QuerySet.stmt() capabilityPlan = %+v, want nil", stmt.capabilityPlan)
				}

				return
			}
			if stmt.capabilityPlan == nil {
				t.Fatal("QuerySet.stmt() capabilityPlan = nil, want a plan")
			}
			if wantColumn := len(tt.checks) > 0; (stmt.capabilityPlan.checksColumn != "") != wantColumn {
				t.Errorf("capabilityPlan.checksColumn = %q, want set: %v", stmt.capabilityPlan.checksColumn, wantColumn)
			}

			got := stmt.capabilityPlan.assemble(tt.checks)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("capabilityPlan.assemble() mismatch (-want +got):\n%s", diff)
			}

			// The strongest byte-identity pin: strip the reserved column and
			// the statements agree with the capability-free rendering.
			if stmt.capabilityPlan.checksColumn == "" && !strings.Contains(tt.wantSQL, capabilityChecksColumnName) && normalizeSQL(stmt.SQL) != baseSQL {
				t.Errorf("data-free capability statement differs from the capability-free statement:\n%s", stmt.SQL)
			}
		})
	}
}

// executeCollection is renderCollection plus the transition vocabulary: two
// RPC method resources whose declared transitions target the enforcement
// resource, and the uniform state attribute their membership booleans lower
// against.
func executeCollection(t *testing.T) *GeneratedCollection {
	t.Helper()

	g, err := NewGeneratedCollection(CollectionData{Resources: []CollectionResource{
		{
			Name:        enforcedResource,
			Scope:       accesstypes.DomainPermissionScope,
			Permissions: []accesstypes.Permission{accesstypes.Read},
			Attributes: []AttributeData{
				{Name: "owner", Column: "Owner", Type: AttributeTypeString},
				{Name: StateAttribute, Column: "State", Type: AttributeTypeString},
			},
			Domain: &DomainBindingData{Column: "Station"},
		},
		{
			Name:        "CancelTask",
			Scope:       accesstypes.DomainPermissionScope,
			Permissions: []accesstypes.Permission{accesstypes.Execute},
			Transition:  &TransitionData{Target: enforcedResource, From: []string{"draft", "scheduled"}, To: "canceled"},
		},
		{
			Name:        "StartTask",
			Scope:       accesstypes.DomainPermissionScope,
			Permissions: []accesstypes.Permission{accesstypes.Execute},
			Transition:  &TransitionData{Target: enforcedResource, From: []string{"scheduled"}, To: "in_progress"},
		},
		{
			// The plain located-row form (§12): a @target with no transition.
			Name:        "NudgeTask",
			Scope:       accesstypes.DomainPermissionScope,
			Permissions: []accesstypes.Permission{accesstypes.Execute},
			Target:      enforcedResource,
		},
	}})
	if err != nil {
		t.Fatalf("NewGeneratedCollection() error = %v", err)
	}

	return g
}

// createCollection is renderCollection plus workflow membership: two member
// resources whose immediate parent hop is the enforcement resource (§11).
func createCollection(t *testing.T) *GeneratedCollection {
	t.Helper()

	g, err := NewGeneratedCollection(CollectionData{Resources: []CollectionResource{
		{
			Name:        enforcedResource,
			Scope:       accesstypes.DomainPermissionScope,
			Permissions: []accesstypes.Permission{accesstypes.Read},
			Attributes: []AttributeData{
				{Name: "owner", Column: "Owner", Type: AttributeTypeString},
				{Name: StateAttribute, Column: "State", Type: AttributeTypeString},
			},
			Domain: &DomainBindingData{Column: "Station"},
		},
		{
			Name:        "TaskLines",
			Scope:       accesstypes.DomainPermissionScope,
			Permissions: []accesstypes.Permission{accesstypes.Create},
			Parent:      enforcedResource,
		},
		{
			Name:        "TaskNotes",
			Scope:       accesstypes.DomainPermissionScope,
			Permissions: []accesstypes.Permission{accesstypes.Create},
			Parent:      enforcedResource,
		},
	}})
	if err != nil {
		t.Fatalf("NewGeneratedCollection() error = %v", err)
	}

	return g
}

// TestQuerySet_stmt_createCapability pins the create-under-parent affordance
// (§11/§13): the Create list names the workflow members the user may create
// beneath the row — an unconditional grant is structural (no SQL), a
// conditional grant renders its state-evaluable residue against the parent's
// own uniform state binding, every other term counts potentially-true, and an
// ungranted member never appears.
func TestQuerySet_stmt_createCapability(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	baseSQL := "SELECT Id, Public, Tagged FROM enforcementResources WHERE (`enforcementResources`.`Station` = @domain)"

	tests := []struct {
		name       string
		collection func(*testing.T) *GeneratedCollection
		byPerm     map[accesstypes.Permission]accesstypes.Decisions
		wantSQL    string
		checks     []bool
		want       map[accesstypes.Permission]any
	}{
		{
			name:       "an unconditional member grant is structural",
			collection: createCollection,
			byPerm: map[accesstypes.Permission]accesstypes.Decisions{
				accesstypes.Create: {"TaskLines": accesstypes.Granted()},
			},
			wantSQL: baseSQL,
			want:    map[accesstypes.Permission]any{accesstypes.Create: []string{"TaskLines"}},
		},
		{
			// The member's synthesized state binding is its parent's state, so
			// the condition lowers against this row's own state column.
			name:       "a state-conditioned member grant gates per row",
			collection: createCollection,
			byPerm: map[accesstypes.Permission]accesstypes.Decisions{
				accesstypes.Create: {"TaskLines": conditionalOn("TaskLines", "state = 'draft'")},
			},
			wantSQL: "SELECT Id, Public, Tagged, ARRAY<BOOL>[(`enforcementResources`.`State` = @_c1)] AS zzCapabilityChecks " +
				"FROM enforcementResources WHERE (`enforcementResources`.`Station` = @domain)",
			checks: []bool{false},
			want:   map[accesstypes.Permission]any{accesstypes.Create: []string{}},
		},
		{
			// A term the parent row cannot answer — a column the created row
			// would carry — counts potentially-true while the state residue
			// still renders (§13's fail-open posture, per term).
			name:       "a non-state conjunct assumes true, the state residue renders",
			collection: createCollection,
			byPerm: map[accesstypes.Permission]accesstypes.Decisions{
				accesstypes.Create: {"TaskLines": conditionalOn("TaskLines", "state = 'draft' AND requestedBy = subject")},
			},
			wantSQL: "SELECT Id, Public, Tagged, ARRAY<BOOL>[(`enforcementResources`.`State` = @_c1)] AS zzCapabilityChecks " +
				"FROM enforcementResources WHERE (`enforcementResources`.`Station` = @domain)",
			checks: []bool{true},
			want:   map[accesstypes.Permission]any{accesstypes.Create: []string{"TaskLines"}},
		},
		{
			// A condition with no state term at all folds structural: wholly
			// potentially-true, zero SQL.
			name:       "a wholly unanswerable condition folds structural",
			collection: createCollection,
			byPerm: map[accesstypes.Permission]accesstypes.Decisions{
				accesstypes.Create: {"TaskNotes": conditionalOn("TaskNotes", "new.severity <= 3")},
			},
			wantSQL: baseSQL,
			want:    map[accesstypes.Permission]any{accesstypes.Create: []string{"TaskNotes"}},
		},
		{
			name:       "an ungranted member never appears",
			collection: createCollection,
			byPerm: map[accesstypes.Permission]accesstypes.Decisions{
				accesstypes.Create: {"TaskLines": accesstypes.Granted()},
			},
			wantSQL: baseSQL,
			want:    map[accesstypes.Permission]any{accesstypes.Create: []string{"TaskLines"}},
		},
		{
			name:       "no members answers empty on the byte-identical statement",
			collection: renderCollection,
			byPerm:     map[accesstypes.Permission]accesstypes.Decisions{},
			wantSQL:    baseSQL,
			want:       map[accesstypes.Permission]any{accesstypes.Create: []string{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rSet, err := NewSet[enforcementResource, enforcementReadRequest](accesstypes.Read)
			if err != nil {
				t.Fatalf("NewSet() error = %v", err)
			}

			q := NewQuerySet(NewMetadata[enforcementResource]())
			q.env = accesstypes.EnvironmentAt(now)
			q.jsonNames = map[accesstypes.Field]string{"ID": "id", "Public": "public", "Tagged": "tagged"}
			q.collection = tt.collection(t)
			q.EnableUserPermissionEnforcement(rSet, capStubPermissions{byPerm: tt.byPerm}, testScope, accesstypes.Read)
			for _, field := range []accesstypes.Field{"ID", "Public", "Tagged"} {
				q.AddField(field)
			}
			q.RequestCapabilities(accesstypes.Create)

			if err := q.checkPermissions(t.Context(), SpannerDBType); err != nil {
				t.Fatalf("QuerySet.checkPermissions() error = %v", err)
			}

			stmt, err := q.stmt(SpannerDBType)
			if err != nil {
				t.Fatalf("QuerySet.stmt() error = %v", err)
			}

			if got := normalizeSQL(stmt.SQL); got != tt.wantSQL {
				t.Errorf("QuerySet.stmt() SQL =\n%s\nwant\n%s", got, tt.wantSQL)
			}
			if stmt.capabilityPlan == nil {
				t.Fatal("QuerySet.stmt() capabilityPlan = nil, want a plan")
			}

			got := stmt.capabilityPlan.assemble(tt.checks)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("capabilityPlan.assemble() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestQuerySet_stmt_executeCapability pins the Execute affordance (§09/§13):
// granted transition methods gate on the row's pre-image state membership —
// one shared boolean per distinct from set in the reserved array column —
// an ungranted method never appears, and a resource nothing transitions onto
// answers an empty list on the byte-identical statement.
func TestQuerySet_stmt_executeCapability(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	baseSQL := "SELECT Id, Public, Tagged FROM enforcementResources WHERE (`enforcementResources`.`Station` = @domain)"

	tests := []struct {
		name       string
		collection func(*testing.T) *GeneratedCollection
		byPerm     map[accesstypes.Permission]accesstypes.Decisions
		wantSQL    string
		checks     []bool
		want       map[accesstypes.Permission]any
	}{
		{
			name:       "granted methods gate on membership booleans, one per distinct from set",
			collection: executeCollection,
			byPerm: map[accesstypes.Permission]accesstypes.Decisions{
				accesstypes.Execute: {"CancelTask": accesstypes.Granted(), "StartTask": accesstypes.Granted()},
			},
			wantSQL: "SELECT Id, Public, Tagged, ARRAY<BOOL>[(`enforcementResources`.`State` IN (@_c1, @_c2)), (`enforcementResources`.`State` IN (@_c3))] AS zzCapabilityChecks " +
				"FROM enforcementResources WHERE (`enforcementResources`.`Station` = @domain)",
			checks: []bool{false, true},
			want:   map[accesstypes.Permission]any{accesstypes.Execute: []string{"StartTask"}},
		},
		{
			name:       "an ungranted method never appears",
			collection: executeCollection,
			byPerm: map[accesstypes.Permission]accesstypes.Decisions{
				accesstypes.Execute: {"StartTask": accesstypes.Granted()},
			},
			wantSQL: "SELECT Id, Public, Tagged, ARRAY<BOOL>[(`enforcementResources`.`State` IN (@_c1))] AS zzCapabilityChecks " +
				"FROM enforcementResources WHERE (`enforcementResources`.`Station` = @domain)",
			checks: []bool{true},
			want:   map[accesstypes.Permission]any{accesstypes.Execute: []string{"StartTask"}},
		},
		{
			// §12: a conditional Execute grant on a transition method ANDs its
			// condition into the same boolean as the state membership.
			name:       "a conditional transition grant ANDs its condition with the membership",
			collection: executeCollection,
			byPerm: map[accesstypes.Permission]accesstypes.Decisions{
				accesstypes.Execute: {"StartTask": conditionalOn("StartTask", "owner = subject")},
			},
			wantSQL: "SELECT Id, Public, Tagged, ARRAY<BOOL>[((`enforcementResources`.`State` IN (@_c1) AND `enforcementResources`.`Owner` = @subject))] AS zzCapabilityChecks " +
				"FROM enforcementResources WHERE (`enforcementResources`.`Station` = @domain)",
			checks: []bool{true},
			want:   map[accesstypes.Permission]any{accesstypes.Execute: []string{"StartTask"}},
		},
		{
			// A plain @target method with an unconditional grant is structural:
			// it appears on every row and adds no boolean — no extra SQL.
			name:       "a plain granted method is structural",
			collection: executeCollection,
			byPerm: map[accesstypes.Permission]accesstypes.Decisions{
				accesstypes.Execute: {"NudgeTask": accesstypes.Granted()},
			},
			wantSQL: baseSQL,
			want:    map[accesstypes.Permission]any{accesstypes.Execute: []string{"NudgeTask"}},
		},
		{
			// A conditional grant on a plain method gates on the condition
			// alone — there is no from set to intersect.
			name:       "a plain conditional method gates on its condition alone",
			collection: executeCollection,
			byPerm: map[accesstypes.Permission]accesstypes.Decisions{
				accesstypes.Execute: {"NudgeTask": conditionalOn("NudgeTask", "owner = subject")},
			},
			wantSQL: "SELECT Id, Public, Tagged, ARRAY<BOOL>[(`enforcementResources`.`Owner` = @subject)] AS zzCapabilityChecks " +
				"FROM enforcementResources WHERE (`enforcementResources`.`Station` = @domain)",
			checks: []bool{false},
			want:   map[accesstypes.Permission]any{accesstypes.Execute: []string{}},
		},
		{
			name:       "no declared transitions answers empty on the byte-identical statement",
			collection: renderCollection,
			byPerm:     map[accesstypes.Permission]accesstypes.Decisions{},
			wantSQL:    baseSQL,
			want:       map[accesstypes.Permission]any{accesstypes.Execute: []string{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rSet, err := NewSet[enforcementResource, enforcementReadRequest](accesstypes.Read)
			if err != nil {
				t.Fatalf("NewSet() error = %v", err)
			}

			q := NewQuerySet(NewMetadata[enforcementResource]())
			q.env = accesstypes.EnvironmentAt(now)
			q.jsonNames = map[accesstypes.Field]string{"ID": "id", "Public": "public", "Tagged": "tagged"}
			q.collection = tt.collection(t)
			q.EnableUserPermissionEnforcement(rSet, capStubPermissions{byPerm: tt.byPerm}, testScope, accesstypes.Read)
			for _, field := range []accesstypes.Field{"ID", "Public", "Tagged"} {
				q.AddField(field)
			}
			q.RequestCapabilities(accesstypes.Execute)

			if err := q.checkPermissions(t.Context(), SpannerDBType); err != nil {
				t.Fatalf("QuerySet.checkPermissions() error = %v", err)
			}

			stmt, err := q.stmt(SpannerDBType)
			if err != nil {
				t.Fatalf("QuerySet.stmt() error = %v", err)
			}

			if got := normalizeSQL(stmt.SQL); got != tt.wantSQL {
				t.Errorf("QuerySet.stmt() SQL =\n%s\nwant\n%s", got, tt.wantSQL)
			}
			if stmt.capabilityPlan == nil {
				t.Fatal("QuerySet.stmt() capabilityPlan = nil, want a plan")
			}

			got := stmt.capabilityPlan.assemble(tt.checks)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("capabilityPlan.assemble() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
