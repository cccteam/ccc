package resource

import (
	"context"
	"errors"
	"testing"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

// keyedRow is a resource keyed by a UUID, as most generated resources are.
type keyedRow struct {
	ID   ccc.UUID `spanner:"Id"`
	Name string   `spanner:"Name"`
}

func (keyedRow) Resource() accesstypes.Resource {
	return "KeyedRows"
}

// pairRow is a resource with a compound key, whose read route spells both parts.
type pairRow struct {
	RefitID    string `spanner:"RefitId"`
	TaskNumber int64  `spanner:"TaskNumber"`
	Note       string `spanner:"Note"`
}

func (pairRow) Resource() accesstypes.Resource {
	return "PairRows"
}

func TestRowKey(t *testing.T) {
	t.Parallel()

	id := ccc.Must(ccc.UUIDFromString("70000000-0000-4000-8000-000000000001"))

	tests := []struct {
		name   string
		values []any
		want   string
	}{
		{name: "a UUID key spells as its text form", values: []any{id}, want: "70000000-0000-4000-8000-000000000001"},
		{name: "a string key is itself", values: []any{"anvil"}, want: "anvil"},
		{name: "a compound key joins its parts in route order", values: []any{id, int64(3)}, want: "70000000-0000-4000-8000-000000000001/3"},
		{name: "no values spell as the empty key", values: nil, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := RowKey(tt.values...); got != tt.want {
				t.Errorf("RowKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTouchedRows_record(t *testing.T) {
	t.Parallel()

	id := ccc.Must(ccc.UUIDFromString("70000000-0000-4000-8000-000000000001"))
	keyed := func(patchType PatchType) *PatchSet[keyedRow] {
		p := NewPatchSet(NewMetadata[keyedRow]()).SetPatchType(patchType)
		p.SetKey("ID", id)

		return p
	}
	scoped := func(patchType PatchType, scope accesstypes.Scope) *PatchSet[keyedRow] {
		p := keyed(patchType)
		p.EnableUserPermissionEnforcement(nil, &stubPermissions{user: "alice"}, scope, accesstypes.Update)

		return p
	}
	pair := func(patchType PatchType) *PatchSet[pairRow] {
		p := NewPatchSet(NewMetadata[pairRow]()).SetPatchType(patchType)
		p.SetKey("RefitID", "refit-1")
		p.SetKey("TaskNumber", int64(3))

		return p
	}

	tests := []struct {
		name    string
		patches []PatchSetMetadata
		want    []touchedRow
	}{
		{
			name:    "an update records the row as written",
			patches: []PatchSetMetadata{keyed(UpdatePatchType)},
			want:    []touchedRow{{resource: "KeyedRows", change: RowChange{Key: id.String()}}},
		},
		{
			name:    "a delete records the row as deleted",
			patches: []PatchSetMetadata{keyed(DeletePatchType)},
			want:    []touchedRow{{resource: "KeyedRows", change: RowChange{Key: id.String(), Deleted: true}}},
		},
		{
			name:    "a compound key is spelled as the read route spells it",
			patches: []PatchSetMetadata{pair(CreatePatchType)},
			want:    []touchedRow{{resource: "PairRows", change: RowChange{Key: "refit-1/3"}}},
		},
		{
			name:    "a row written twice is recorded once, with the later write's deleted flag",
			patches: []PatchSetMetadata{keyed(UpdatePatchType), keyed(DeletePatchType)},
			want:    []touchedRow{{resource: "KeyedRows", change: RowChange{Key: id.String(), Deleted: true}}},
		},
		{
			name:    "a patch decoded in a tenant scope carries its domain",
			patches: []PatchSetMetadata{scoped(UpdatePatchType, accesstypes.DomainScope("anvil"))},
			want:    []touchedRow{{resource: "KeyedRows", domain: "anvil", change: RowChange{Key: id.String()}}},
		},
		{
			name:    "a patch decoded in the global scope carries no domain",
			patches: []PatchSetMetadata{scoped(UpdatePatchType, accesstypes.GlobalScope())},
			want:    []touchedRow{{resource: "KeyedRows", change: RowChange{Key: id.String()}}},
		},
		{
			name:    "a change-event row is not a row a page shows",
			patches: []PatchSetMetadata{&DataChangeEvent{TableName: "KeyedRows", RowID: id.String()}, keyed(UpdatePatchType)},
			want:    []touchedRow{{resource: "KeyedRows", change: RowChange{Key: id.String()}}},
		},
		{
			name:    "rows keep first-buffered order across resources",
			patches: []PatchSetMetadata{pair(UpdatePatchType), keyed(UpdatePatchType)},
			want: []touchedRow{
				{resource: "PairRows", change: RowChange{Key: "refit-1/3"}},
				{resource: "KeyedRows", change: RowChange{Key: id.String()}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			record := newTouchedRows()
			for _, patch := range tt.patches {
				record.record(patch)
			}
			if diff := cmp.Diff(tt.want, record.list(), cmp.AllowUnexported(touchedRow{})); diff != "" {
				t.Errorf("list() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTouchedRows_Rows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rows     []touchedRow
		fallback accesstypes.Domain
		want     map[accesstypes.Domain]map[accesstypes.Resource][]RowChange
	}{
		{
			name:     "nothing committed groups to nothing",
			fallback: "anvil",
			want:     map[accesstypes.Domain]map[accesstypes.Resource][]RowChange{},
		},
		{
			name: "rows without a domain take the request's",
			rows: []touchedRow{
				{resource: "Ships", change: RowChange{Key: "s1"}},
				{resource: "Ships", change: RowChange{Key: "s2", Deleted: true}},
				{resource: "Refits", change: RowChange{Key: "r1"}},
			},
			fallback: "anvil",
			want: map[accesstypes.Domain]map[accesstypes.Resource][]RowChange{
				"anvil": {
					"Ships":  {{Key: "s1"}, {Key: "s2", Deleted: true}},
					"Refits": {{Key: "r1"}},
				},
			},
		},
		{
			name: "rows decoded in their own tenant keep it beside the request's",
			rows: []touchedRow{
				{resource: "Hangars", domain: "bastion", change: RowChange{Key: "h1"}},
				{resource: "Clients", change: RowChange{Key: "c1"}},
			},
			fallback: "",
			want: map[accesstypes.Domain]map[accesstypes.Resource][]RowChange{
				"bastion": {"Hangars": {{Key: "h1"}}},
				"":        {"Clients": {{Key: "c1"}}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			collector := &TouchedRows{}
			collector.add(tt.rows)
			if got := collector.Empty(); got != (len(tt.rows) == 0) {
				t.Errorf("Empty() = %v, want %v", got, len(tt.rows) == 0)
			}
			if diff := cmp.Diff(tt.want, collector.Rows(tt.fallback)); diff != "" {
				t.Errorf("Rows() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestMockClient_ExecuteFunc_collectsTouchedRows(t *testing.T) {
	t.Parallel()

	id := ccc.Must(ccc.UUIDFromString("70000000-0000-4000-8000-000000000001"))

	tests := []struct {
		name string
		// fnErr is what the function returns after buffering a patch.
		fnErr error
		// collect puts a collector in the context the function runs under.
		collect bool
		wantErr bool
		want    map[accesstypes.Domain]map[accesstypes.Resource][]RowChange
	}{
		{
			name:    "a committed function's rows reach the collector",
			collect: true,
			want:    map[accesstypes.Domain]map[accesstypes.Resource][]RowChange{"anvil": {"KeyedRows": {{Key: id.String()}}}},
		},
		{
			name:    "a function that errors committed nothing, so nothing is collected",
			fnErr:   errors.New("body failed"),
			collect: true,
			wantErr: true,
			want:    map[accesstypes.Domain]map[accesstypes.Resource][]RowChange{},
		},
		{
			name:    "a dry run committed nothing",
			fnErr:   ErrDryRun,
			collect: true,
			wantErr: true,
			want:    map[accesstypes.Domain]map[accesstypes.Resource][]RowChange{},
		},
		{
			name: "a context without a collector collects nothing and the call still succeeds",
			want: map[accesstypes.Domain]map[accesstypes.Resource][]RowChange{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := NewMockClient(&bufferingTxn{}, nil, nil)
			ctx := t.Context()
			var collector *TouchedRows
			if tt.collect {
				ctx, collector = CollectTouchedRows(ctx)
			}

			err := client.ExecuteFunc(ctx, func(ctx context.Context, txn ReadWriteTransaction) error {
				p := NewPatchSet(NewMetadata[keyedRow]()).SetPatchType(CreatePatchType)
				p.SetKey("ID", id)
				p.Set("Name", "Kingfisher")
				if err := p.Buffer(ctx, txn); err != nil {
					t.Fatalf("Buffer() error = %v", err)
				}

				return tt.fnErr
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("ExecuteFunc() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, collector.Rows("anvil")); diff != "" {
				t.Errorf("Rows() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestQuerySet_Permitted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		perms   UserPermissions
		want    bool
		wantErr bool
	}{
		{name: "a granted gate permits", perms: &stubPermissions{user: "alice"}, want: true},
		{name: "a conditional grant permits: the row rules run when the query does", perms: &decidingPermissions{decision: accesstypes.Conditional(accesstypes.ConditionGroup{})}, want: true},
		{name: "a denied gate does not permit", perms: &decidingPermissions{decision: accesstypes.Denied()}, want: false},
		{name: "a checker that fails fails the question", perms: &stubPermissions{err: errors.New("engine down")}, wantErr: true},
		{name: "a query bound to no permissions is not permitted", perms: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			q := NewQuerySet(NewMetadata[keyedRow]())
			if tt.perms != nil {
				q.EnableUserPermissionEnforcement(nil, tt.perms, accesstypes.DomainScope("anvil"), accesstypes.List)
			}
			got, err := q.Permitted(t.Context())
			if (err != nil) != tt.wantErr {
				t.Fatalf("Permitted() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("Permitted() = %v, want %v", got, tt.want)
			}
		})
	}
}

// decidingPermissions answers every check with one decision.
type decidingPermissions struct {
	stubPermissions
	decision accesstypes.Decision
}

func (d *decidingPermissions) Check(_ context.Context, _ accesstypes.Environment, _ accesstypes.Scope, _ accesstypes.Permission, resources ...accesstypes.Resource) (accesstypes.Decisions, error) {
	decisions := make(accesstypes.Decisions, len(resources))
	for _, res := range resources {
		decisions[res] = d.decision
	}

	return decisions, nil
}
