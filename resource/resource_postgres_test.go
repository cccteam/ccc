package resource

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cloud.google.com/go/civil"
	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
)

// The Postgres runtime's fixture resources, over testdata/postgres/schema. Each field
// carries both database tags, as a generated resource does.

// pgAttrs is a type stored in a JSON column the way the generator stores one: its
// storage methods are the Spanner client's Encoder and Decoder, which the Postgres
// runtime reads as well.
type pgAttrs struct {
	Color string `json:"color"`
	Size  int    `json:"size"`
}

// EncodeSpanner is the generated Encoder: the value, stored as the JSON it marshals to.
func (v pgAttrs) EncodeSpanner() (any, error) {
	return spanner.NullJSON{Value: v, Valid: true}, nil
}

// DecodeSpanner is the generated Decoder: the JSON text of the column, the zero value for NULL.
func (v *pgAttrs) DecodeSpanner(val any) error {
	var raw string
	switch x := val.(type) {
	case nil:
		*v = pgAttrs{}

		return nil
	case string:
		raw = x
	default:
		return errors.Newf("pgAttrs.DecodeSpanner(): expected the column's JSON as a string, got %T", val)
	}

	if err := json.Unmarshal([]byte(raw), v); err != nil {
		return errors.Wrap(err, "json.Unmarshal()")
	}

	return nil
}

const pgGadgetsTable = accesstypes.Resource("Gadgets")

// pgGadget is a row of the Gadgets table.
type pgGadget struct {
	ID       ccc.UUID            `spanner:"Id"       postgres:"Id"`
	Name     string              `spanner:"Name"     postgres:"Name"`
	Note     *string             `spanner:"Note"     postgres:"Note"`
	Weight   int64               `spanner:"Weight"   postgres:"Weight"`
	Pieces   *int64              `spanner:"Pieces"   postgres:"Pieces"`
	Ratio    float64             `spanner:"Ratio"    postgres:"Ratio"`
	Price    decimal.Decimal     `spanner:"Price"    postgres:"Price"`
	Fee      decimal.NullDecimal `spanner:"Fee"      postgres:"Fee"`
	Active   *bool               `spanner:"Active"   postgres:"Active"`
	Seen     time.Time           `spanner:"Seen"     postgres:"Seen"`
	Retired  *time.Time          `spanner:"Retired"  postgres:"Retired"`
	Made     civil.Date          `spanner:"Made"     postgres:"Made"`
	Scrapped *civil.Date         `spanner:"Scrapped" postgres:"Scrapped"`
	Attrs    *pgAttrs            `spanner:"Attrs"    postgres:"Attrs"`
	Tags     []string            `spanner:"Tags"     postgres:"Tags"`
	Owner    ccc.NullUUID        `spanner:"Owner"    postgres:"Owner"`
}

func (pgGadget) Resource() accesstypes.Resource { return pgGadgetsTable }

// DefaultConfig tracks the row's changes, so a write also writes a change event.
func (pgGadget) DefaultConfig() Config { return Config{TrackChanges: true} }

// pgWidget is a row of the Widgets table, which references a gadget.
type pgWidget struct {
	ID       string   `spanner:"Id"       postgres:"Id"`
	GadgetID ccc.UUID `spanner:"GadgetId" postgres:"GadgetId"`
	Label    string   `spanner:"Label"    postgres:"Label"`
}

func (pgWidget) Resource() accesstypes.Resource { return "Widgets" }

func (pgWidget) DefaultConfig() Config { return Config{} }

// pgBolt is a row of the Bolts table, whose key is composite.
type pgBolt struct {
	WidgetID string              `spanner:"WidgetId" postgres:"WidgetId"`
	Seq      int64               `spanner:"Seq"      postgres:"Seq"`
	Torque   decimal.NullDecimal `spanner:"Torque"   postgres:"Torque"`
}

func (pgBolt) Resource() accesstypes.Resource { return "Bolts" }

func (pgBolt) DefaultConfig() Config { return Config{} }

// testDatabases numbers the databases the semantic and Postgres tests create, so tests that
// run together, or a test run again in one process (-count), find their names free: the
// container outlives the tests.
var testDatabases atomic.Int64

// postgresDatabase creates a database on the shared container migrated with the
// fixture's tables, dropped with the container when the test binary exits, and the
// resource client over it.
func postgresDatabase(t *testing.T, name string) (*initiator.PostgresDatabase, *PostgresClient) {
	t.Helper()

	container := postgresContainer(t)
	db, err := container.CreateDatabase(t.Context(), fmt.Sprintf("%s-%d", name, testDatabases.Add(1)))
	if err != nil {
		t.Fatalf("initiator.PostgresContainer.CreateDatabase() error = %v", err)
	}
	t.Cleanup(db.Close)
	if err := db.MigrateUp("file://testdata/postgres/schema"); err != nil {
		t.Fatalf("initiator.PostgresDatabase.MigrateUp() error = %v", err)
	}

	return db, NewPostgresClient(db.Pool)
}

// pgComparers compare the fixture rows: decimals by value, instants as instants.
var pgComparers = []cmp.Option{
	cmp.Comparer(func(a, b decimal.Decimal) bool { return a.Equal(b) }),
	cmp.Comparer(time.Time.Equal),
}

// mustCCCUUID parses a UUID for a fixture.
func mustCCCUUID(t *testing.T, s string) ccc.UUID {
	t.Helper()

	id, err := ccc.UUIDFromString(s)
	if err != nil {
		t.Fatalf("ccc.UUIDFromString() error = %v", err)
	}

	return id
}

// createGadget writes a gadget through a create patch.
func createGadget(ctx context.Context, client Client, g *pgGadget) error {
	p := NewPatchSet(NewMetadata[pgGadget]()).SetPatchType(CreatePatchType)
	p.SetKey("ID", g.ID)
	p.Set("Name", g.Name).Set("Note", g.Note).Set("Weight", g.Weight).Set("Pieces", g.Pieces).Set("Ratio", g.Ratio)
	p.Set("Price", g.Price).Set("Fee", g.Fee).Set("Active", g.Active).Set("Seen", g.Seen).Set("Retired", g.Retired)
	p.Set("Made", g.Made).Set("Scrapped", g.Scrapped).Set("Attrs", g.Attrs).Set("Tags", g.Tags).Set("Owner", g.Owner)

	return p.Apply(ctx, client, "test")
}

// readGadget reads a gadget by key through a QuerySet.
func readGadget(ctx context.Context, txn ReadOnlyTransaction, id ccc.UUID) (*pgGadget, error) {
	qSet := NewQuerySet(NewMetadata[pgGadget]())
	qSet.SetKey("ID", id)
	for _, field := range NewMetadata[pgGadget]().DBFields(PostgresDBType) {
		qSet.AddField(field)
	}
	row, err := qSet.Read(ctx, txn)
	if err != nil {
		return nil, err
	}

	return &row.Data, nil
}

// listGadgets lists every gadget, ordered by name.
func listGadgets(ctx context.Context, txn ReadOnlyTransaction, limit *uint64) ([]pgGadget, error) {
	qSet := NewQuerySet(NewMetadata[pgGadget]())
	for _, field := range NewMetadata[pgGadget]().DBFields(PostgresDBType) {
		qSet.AddField(field)
	}
	qSet.SetSortFields([]SortField{{Field: "Name"}})
	qSet.SetLimit(limit)

	var gadgets []pgGadget
	for row, err := range qSet.List(ctx, txn) {
		if err != nil {
			return nil, err
		}
		gadgets = append(gadgets, row.Data)
	}

	return gadgets, nil
}

// TestPostgresClient_roundTrip pins a row's trip through the Postgres runtime: every
// column type a resource field reads is written by a create patch and read back as it
// was written, then updated, upserted, listed, counted and deleted.
func TestPostgresClient_roundTrip(t *testing.T) {
	t.Parallel()

	db, client := postgresDatabase(t, "pg-roundtrip")
	ctx := t.Context()
	id := mustCCCUUID(t, "8a6570c8-1e51-4870-9def-3f68d0447d09")
	seen := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

	want := &pgGadget{
		ID: id, Name: "sprocket", Note: ptr("first"), Weight: 12, Pieces: ptr(int64(3)), Ratio: 1.5,
		Price: decimal.RequireFromString("19.95"), Fee: decimal.NullDecimal{Decimal: decimal.RequireFromString("0.25"), Valid: true},
		Active: ptr(true), Seen: seen, Retired: ptr(seen.Add(time.Hour)),
		Made: civil.Date{Year: 2026, Month: time.January, Day: 2}, Scrapped: ptr(civil.Date{Year: 2027, Month: time.February, Day: 3}),
		Attrs: &pgAttrs{Color: "red", Size: 4}, Tags: []string{"a", "b"}, Owner: ccc.NullUUIDFromUUID(mustCCCUUID(t, "99999999-9999-4999-8999-999999999999")),
	}
	if err := createGadget(ctx, client, want); err != nil {
		t.Fatalf("create error = %v", err)
	}

	got, err := readGadget(ctx, client, id)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if diff := cmp.Diff(want, got, pgComparers...); diff != "" {
		t.Errorf("Read() mismatch (-want +got):\n%s", diff)
	}

	// An update writes the columns it names, NULLs a nullable one, and leaves the rest.
	p := NewPatchSet(NewMetadata[pgGadget]()).SetPatchType(UpdatePatchType)
	p.SetKey("ID", id)
	p.Set("Name", "cog").Set("Note", (*string)(nil)).Set("Fee", decimal.NullDecimal{}).Set("Weight", int64(20)).Set("Owner", ccc.NullUUID{})
	if err := p.Apply(ctx, client, "test"); err != nil {
		t.Fatalf("update error = %v", err)
	}
	want.Name, want.Note, want.Fee, want.Weight, want.Owner = "cog", nil, decimal.NullDecimal{}, 20, ccc.NullUUID{}
	got, err = readGadget(ctx, client, id)
	if err != nil {
		t.Fatalf("Read() after update error = %v", err)
	}
	if diff := cmp.Diff(want, got, pgComparers...); diff != "" {
		t.Errorf("Read() after update mismatch (-want +got):\n%s", diff)
	}

	// A create-or-update inserts a row that is not there and updates one that is.
	other := mustCCCUUID(t, "11111111-1111-4111-8111-111111111111")
	upsert := func(id ccc.UUID, name string) {
		t.Helper()

		p := NewPatchSet(NewMetadata[pgGadget]()).SetPatchType(CreateOrUpdatePatchType)
		p.SetKey("ID", id)
		p.Set("Name", name).Set("Weight", int64(1)).Set("Ratio", 2.0).Set("Price", decimal.NewFromInt(1)).Set("Seen", seen).Set("Made", civil.Date{Year: 2026, Month: time.May, Day: 6})
		if err := p.Apply(ctx, client, "test"); err != nil {
			t.Fatalf("create-or-update error = %v", err)
		}
	}
	upsert(other, "bearing")
	upsert(other, "axle")

	gadgets, err := listGadgets(ctx, client, nil)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if names := []string{gadgets[0].Name, gadgets[1].Name}; len(gadgets) != 2 || names[0] != "axle" || names[1] != "cog" {
		t.Errorf("List() names = %v, want [axle cog]", names)
	}
	if page, err := listGadgets(ctx, client, ptr(uint64(1))); err != nil || len(page) != 1 || page[0].Name != "axle" {
		t.Errorf("List(limit 1) = %v, %v, want [axle]", page, err)
	}

	count, err := NewQuerySet(NewMetadata[pgGadget]()).Count(ctx, client)
	if err != nil || count != 2 {
		t.Errorf("Count() = %d, %v, want 2", count, err)
	}

	// A delete removes the row, and a read of it is NotFound.
	del := NewPatchSet(NewMetadata[pgGadget]()).SetPatchType(DeletePatchType)
	del.SetKey("ID", id)
	if err := del.Apply(ctx, client, "test"); err != nil {
		t.Fatalf("delete error = %v", err)
	}
	if _, err := readGadget(ctx, client, id); !httpio.HasNotFound(err) {
		t.Errorf("Read() after delete error = %v, want NotFound", err)
	}

	// Every write of a tracked resource wrote its change event beside it.
	var events int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM "DataChangeEvents" WHERE "TableName" = $1`, string(pgGadgetsTable)).Scan(&events); err != nil {
		t.Fatalf("count events error = %v", err)
	}
	if events != 5 {
		t.Errorf("change events = %d, want 5 (create, update, the two upserts, delete)", events)
	}
}

// TestPostgresClient_refusals pins the 4xx each constraint a write can break answers,
// the sentences of the Spanner client's translation, and that the refused transaction
// leaves nothing behind.
func TestPostgresClient_refusals(t *testing.T) {
	t.Parallel()

	db, client := postgresDatabase(t, "pg-refusals")
	ctx := t.Context()
	seen := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	id := mustCCCUUID(t, "8a6570c8-1e51-4870-9def-3f68d0447d09")
	gadget := func(id ccc.UUID, name string, weight int64) *pgGadget {
		return &pgGadget{ID: id, Name: name, Weight: weight, Ratio: 1, Price: decimal.NewFromInt(1), Seen: seen, Made: civil.Date{Year: 2026, Month: time.May, Day: 6}}
	}
	if err := createGadget(ctx, client, gadget(id, "sprocket", 1)); err != nil {
		t.Fatalf("create error = %v", err)
	}

	tests := []struct {
		name    string
		write   func() error
		check   func(error) bool
		wantMsg string
	}{
		{
			name:    "a duplicate key",
			write:   func() error { return createGadget(ctx, client, gadget(id, "other", 1)) },
			check:   httpio.HasConflict,
			wantMsg: "Gadgets: a record with this key or a unique value already exists.",
		},
		{
			name: "a duplicate unique value",
			write: func() error {
				return createGadget(ctx, client, gadget(mustCCCUUID(t, "22222222-2222-4222-8222-222222222222"), "sprocket", 1))
			},
			check:   httpio.HasConflict,
			wantMsg: "Gadgets: a record with this key or a unique value already exists.",
		},
		{
			name: "a violated CHECK",
			write: func() error {
				return createGadget(ctx, client, gadget(mustCCCUUID(t, "33333333-3333-4333-8333-333333333333"), "heavy", -1))
			},
			check:   httpio.HasBadRequest,
			wantMsg: "Gadgets: a value is outside the range the record allows.",
		},
		{
			name: "a foreign key that names no row",
			write: func() error {
				p := NewPatchSet(NewMetadata[pgWidget]()).SetPatchType(CreatePatchType)
				p.SetKey("ID", "w1")
				p.Set("GadgetID", mustCCCUUID(t, "44444444-4444-4444-8444-444444444444")).Set("Label", "nut")

				return p.Apply(ctx, client, "test")
			},
			check:   httpio.HasConflict,
			wantMsg: "Widgets: a referenced record does not exist, or a value is too long for its field.",
		},
		{
			name: "an update of a row that does not exist",
			write: func() error {
				p := NewPatchSet(NewMetadata[pgWidget]()).SetPatchType(UpdatePatchType)
				p.SetKey("ID", "ghost")
				p.Set("Label", "ghost")

				return p.Apply(ctx, client, "test")
			},
			check:   httpio.HasNotFound,
			wantMsg: "Widgets: this record does not exist.",
		},
		{
			name: "a delete of a row another row references",
			write: func() error {
				p := NewPatchSet(NewMetadata[pgWidget]()).SetPatchType(CreatePatchType)
				p.SetKey("ID", "w2")
				p.Set("GadgetID", id).Set("Label", "washer")
				if err := p.Apply(ctx, client, "test"); err != nil {
					return err
				}
				del := NewPatchSet(NewMetadata[pgGadget]()).SetPatchType(DeletePatchType)
				del.SetKey("ID", id)

				return del.Apply(ctx, client, "test")
			},
			check:   httpio.HasConflict,
			wantMsg: "Gadgets: this record cannot be deleted while other records still reference it.",
		},
	}
	// None of the refusals leaves a gadget or a change event behind: the check runs when
	// every refusal, which run together, has finished.
	t.Cleanup(func() {
		var gadgets, events int
		if err := db.QueryRow(context.Background(), `SELECT (SELECT COUNT(*) FROM "Gadgets"), (SELECT COUNT(*) FROM "DataChangeEvents")`).Scan(&gadgets, &events); err != nil {
			t.Errorf("count error = %v", err)

			return
		}
		if gadgets != 1 || events != 1 {
			t.Errorf("rows = %d gadgets, %d events, want 1 and 1 (the first create's)", gadgets, events)
		}
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.write()
			if err == nil || !tt.check(err) {
				t.Fatalf("write error = %v, want the refusal's status", err)
			}
			if msg := httpio.Message(err); !strings.Contains(msg, tt.wantMsg) {
				t.Errorf("message = %q, want it to contain %q", msg, tt.wantMsg)
			}
		})
	}
}

// boltsFixture is a database for one ExecuteFunc case: the client over it, how a function
// buffers a bolt, and how the case counts the bolts of one widget.
type boltsFixture struct {
	client *PostgresClient
	insert func(ctx context.Context, txn ReadWriteTransaction, widget string, seq int64) error
	seqs   func(widget string) []int64
}

// newBoltsFixture creates the case's own database, so the cases run together without
// conflicting over a table.
func newBoltsFixture(t *testing.T, name string) *boltsFixture {
	t.Helper()

	db, client := postgresDatabase(t, name)

	return &boltsFixture{
		client: client,
		insert: func(ctx context.Context, txn ReadWriteTransaction, widget string, seq int64) error {
			p := NewPatchSet(NewMetadata[pgBolt]()).SetPatchType(CreatePatchType)
			p.SetKey("WidgetID", widget).SetKey("Seq", seq)
			p.Set("Torque", decimal.NullDecimal{Decimal: decimal.NewFromInt(seq), Valid: true})

			return p.Buffer(ctx, txn)
		},
		seqs: func(widget string) []int64 {
			t.Helper()

			rows, err := db.Query(t.Context(), `SELECT "Seq" FROM "Bolts" WHERE "WidgetId" = $1 ORDER BY "Seq"`, widget)
			if err != nil {
				t.Fatalf("query error = %v", err)
			}
			defer rows.Close()

			var seqs []int64
			for rows.Next() {
				var seq int64
				if err := rows.Scan(&seq); err != nil {
					t.Fatalf("scan error = %v", err)
				}
				seqs = append(seqs, seq)
			}

			return seqs
		},
	}
}

// TestPostgresClient_ExecuteFunc pins the transaction's contract: a function that fails
// rolls back the writes it made, writes are applied when the function returns and its
// reads do not see them, only a commit publishes the rows it wrote, a transaction that
// lost to a concurrent one runs the function again over a fresh transaction, and one that
// keeps losing stops at the bound.
func TestPostgresClient_ExecuteFunc(t *testing.T) {
	t.Parallel()

	t.Run("a function that fails rolls back its writes", func(t *testing.T) {
		t.Parallel()

		f := newBoltsFixture(t, "pg-execute-fails")
		err := f.client.ExecuteFunc(t.Context(), func(ctx context.Context, txn ReadWriteTransaction) error {
			if err := f.insert(ctx, txn, "a", 1); err != nil {
				return err
			}

			return ErrDryRun
		})
		if !errors.Is(err, ErrDryRun) {
			t.Fatalf("ExecuteFunc() error = %v, want ErrDryRun", err)
		}
		if seqs := f.seqs("a"); len(seqs) != 0 {
			t.Errorf("bolts = %v, want none", seqs)
		}
	})

	t.Run("a function's reads do not see the writes it buffered", func(t *testing.T) {
		t.Parallel()

		f := newBoltsFixture(t, "pg-execute-reads")
		err := f.client.ExecuteFunc(t.Context(), func(ctx context.Context, txn ReadWriteTransaction) error {
			if err := f.insert(ctx, txn, "b", 1); err != nil {
				return err
			}
			qSet := NewQuerySet(NewMetadata[pgBolt]())
			qSet.SetKey("WidgetID", "b")
			qSet.SetKey("Seq", int64(1))
			qSet.AddField("Torque")
			if _, err := qSet.Read(ctx, txn); !httpio.HasNotFound(err) {
				t.Errorf("Read() of a buffered write error = %v, want NotFound, as on Spanner", err)
			}

			return nil
		})
		if err != nil {
			t.Fatalf("ExecuteFunc() error = %v", err)
		}
		if diff := cmp.Diff([]int64{1}, f.seqs("b")); diff != "" {
			t.Errorf("the buffered write was not applied at commit (-want +got):\n%s", diff)
		}
	})

	t.Run("only a committed transaction publishes the rows it wrote", func(t *testing.T) {
		t.Parallel()

		f := newBoltsFixture(t, "pg-execute-touched")
		ctx, collector := CollectTouchedRows(t.Context())
		err := f.client.ExecuteFunc(ctx, func(ctx context.Context, txn ReadWriteTransaction) error {
			if err := f.insert(ctx, txn, "d", 1); err != nil {
				return err
			}

			return ErrDryRun
		})
		if !errors.Is(err, ErrDryRun) || !collector.Empty() {
			t.Fatalf("a rolled-back transaction: error = %v, collector empty = %v, want ErrDryRun and an empty collector", err, collector.Empty())
		}

		if err := f.client.ExecuteFunc(ctx, func(ctx context.Context, txn ReadWriteTransaction) error {
			return f.insert(ctx, txn, "d", 2)
		}); err != nil {
			t.Fatalf("ExecuteFunc() error = %v", err)
		}
		want := map[accesstypes.Domain]map[accesstypes.Resource][]RowChange{
			"": {"Bolts": {{Key: RowKey("d", int64(2))}}},
		}
		if diff := cmp.Diff(want, collector.Rows("")); diff != "" {
			t.Errorf("collected rows mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a lost transaction runs again over a fresh one", func(t *testing.T) {
		t.Parallel()

		f := newBoltsFixture(t, "pg-execute-retry")
		attempts := 0
		err := f.client.ExecuteFunc(t.Context(), func(ctx context.Context, txn ReadWriteTransaction) error {
			attempts++
			if err := f.insert(ctx, txn, "c", int64(attempts)); err != nil {
				return err
			}
			if attempts == 1 {
				return &pgconn.PgError{Code: pgSerializationFailure}
			}

			return nil
		})
		if err != nil || attempts != 2 {
			t.Fatalf("ExecuteFunc() = %v after %d attempts, want nil after 2", err, attempts)
		}
		if diff := cmp.Diff([]int64{2}, f.seqs("c")); diff != "" {
			t.Errorf("the committed attempt's bolts mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a transaction that keeps losing stops at the bound", func(t *testing.T) {
		t.Parallel()

		f := newBoltsFixture(t, "pg-execute-bound")
		f.client.retryBackoff = 0
		attempts := 0
		err := f.client.ExecuteFunc(t.Context(), func(context.Context, ReadWriteTransaction) error {
			attempts++

			return &pgconn.PgError{Code: pgSerializationFailure}
		})
		if err == nil || attempts != postgresMaxAttempts {
			t.Errorf("ExecuteFunc() = %v after %d attempts, want an error after %d", err, attempts, postgresMaxAttempts)
		}
	})
}

// TestPostgresReadOnlyTransaction_snapshot pins that a read-only transaction reads one
// snapshot: a row committed after its first read is not seen by its second.
func TestPostgresReadOnlyTransaction_snapshot(t *testing.T) {
	t.Parallel()

	_, client := postgresDatabase(t, "pg-snapshot")
	ctx := t.Context()
	seen := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	gadget := func(id, name string) *pgGadget {
		return &pgGadget{ID: mustCCCUUID(t, id), Name: name, Ratio: 1, Price: decimal.NewFromInt(1), Seen: seen, Made: civil.Date{Year: 2026, Month: time.May, Day: 6}}
	}
	if err := createGadget(ctx, client, gadget("8a6570c8-1e51-4870-9def-3f68d0447d09", "one")); err != nil {
		t.Fatalf("create error = %v", err)
	}

	txn := client.ReadOnlyTransaction()
	defer txn.Close()

	count := func() int64 {
		t.Helper()

		n, err := NewQuerySet(NewMetadata[pgGadget]()).Count(ctx, txn)
		if err != nil {
			t.Fatalf("Count() error = %v", err)
		}

		return n
	}
	if n := count(); n != 1 {
		t.Fatalf("first Count() = %d, want 1", n)
	}
	if err := createGadget(ctx, client, gadget("11111111-1111-4111-8111-111111111111", "two")); err != nil {
		t.Fatalf("second create error = %v", err)
	}
	if n := count(); n != 1 {
		t.Errorf("Count() inside the snapshot = %d, want 1", n)
	}
	if n, err := NewQuerySet(NewMetadata[pgGadget]()).Count(ctx, client); err != nil || n != 2 {
		t.Errorf("Count() outside the snapshot = %d, %v, want 2", n, err)
	}
}

// TestPostgresClient_ExecuteFunc_concurrent pins the serializable transaction under real
// contention: transactions that each read a row and write it back lose to one another
// (SQLSTATE 40001), run again, and in the end every one of them counted.
func TestPostgresClient_ExecuteFunc_concurrent(t *testing.T) {
	t.Parallel()

	_, client := postgresDatabase(t, "pg-concurrent")
	ctx := t.Context()
	id := mustCCCUUID(t, "8a6570c8-1e51-4870-9def-3f68d0447d09")
	seen := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	if err := createGadget(ctx, client, &pgGadget{ID: id, Name: "counter", Ratio: 1, Price: decimal.NewFromInt(1), Seen: seen, Made: civil.Date{Year: 2026, Month: time.May, Day: 6}}); err != nil {
		t.Fatalf("create error = %v", err)
	}

	const workers = 6
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			errs <- client.ExecuteFunc(ctx, func(ctx context.Context, txn ReadWriteTransaction) error {
				qSet := NewQuerySet(NewMetadata[pgGadget]())
				qSet.SetKey("ID", id)
				qSet.AddField("Weight")
				row, err := qSet.Read(ctx, txn)
				if err != nil {
					return err
				}

				p := NewPatchSet(NewMetadata[pgGadget]()).SetPatchType(UpdatePatchType)
				p.SetKey("ID", id)
				p.Set("Weight", row.Data.Weight+1)

				return p.Buffer(ctx, txn, "test")
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("ExecuteFunc() error = %v", err)
		}
	}

	got, err := readGadget(ctx, client, id)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got.Weight != workers {
		t.Errorf("Weight = %d, want %d: a transaction's increment was lost", got.Weight, workers)
	}
}
