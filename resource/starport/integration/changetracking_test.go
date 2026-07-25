package integration

// This suite covers change tracking (resource.Config.TrackChanges), enabled for
// SupplyCrates only via its hand-written Config method. Every tracked mutation writes a
// DataChangeEvents row in the same transaction; untracked resources write none.
//
// NOTE: PII fields are currently recorded unredacted in ChangeSet — the assertions here
// pin that behavior deliberately; if redaction is ever added, update these cases.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	initiator "github.com/cccteam/db-initiator"
	"github.com/go-playground/errors/v5"
)

type changeEvent struct {
	eventSource string
	changeSet   map[string]any
}

// diff returns the {"Old":…,"New":…} pair recorded for a field, failing if absent.
func (e changeEvent) diff(t *testing.T, field string) map[string]any {
	t.Helper()

	d, ok := e.changeSet[field].(map[string]any)
	if !ok {
		t.Fatalf("ChangeSet has no entry for field %q: %v", field, e.changeSet)
	}

	return d
}

// readChangeEvents returns the DataChangeEvents rows for one tracked row, oldest first.
func readChangeEvents(ctx context.Context, t *testing.T, db *initiator.SpannerDB, table, rowID string) []changeEvent {
	t.Helper()

	it := db.Single().Query(ctx, spanner.Statement{
		SQL:    "SELECT EventSource, ChangeSet FROM DataChangeEvents WHERE TableName = @table AND RowId = @rowId ORDER BY EventTime",
		Params: map[string]any{"table": table, "rowId": rowID},
	})

	var events []changeEvent
	err := it.Do(func(row *spanner.Row) error {
		var (
			source    string
			changeSet spanner.NullJSON
		)
		if err := row.Columns(&source, &changeSet); err != nil {
			return errors.Wrap(err, "spanner.Row.Columns()")
		}

		event := changeEvent{eventSource: source}
		if changeSet.Valid {
			event.changeSet, _ = changeSet.Value.(map[string]any)
		}
		events = append(events, event)

		return nil
	})
	if err != nil {
		t.Fatalf("DataChangeEvents query: %v", err)
	}

	return events
}

func TestChangeTracking(t *testing.T) {
	t.Parallel()

	crateCreateGrants := grants{accesstypes.Create: {
		supplyCratesResource,
		fieldResource(supplyCratesResource, "label"),
		fieldResource(supplyCratesResource, "quantity"),
		fieldResource(supplyCratesResource, "priority"),
		fieldResource(supplyCratesResource, "inspectorBadge"),
	}}

	crateUpdateGrants := grants{accesstypes.Update: {
		supplyCratesResource,
		fieldResource(supplyCratesResource, "quantity"),
	}}

	tests := []struct {
		name       string
		grants     grants
		body       string
		wantStatus int
		verify     func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, respBody []byte)
	}{
		{
			name:       "create writes an insert event with nil old values",
			grants:     crateCreateGrants,
			body:       `[{"op":"add","path":"/supply-crates","value":{"label":"Med Kits","quantity":30,"priority":1,"inspectorBadge":"INSP-99"}}]`,
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, respBody []byte) {
				id := createdID(t, respBody, "supplyCrates")
				events := readChangeEvents(ctx, t, db, "SupplyCrates", id)
				if len(events) != 1 {
					t.Fatalf("event count = %d, want 1", len(events))
				}

				ev := events[0]
				if !strings.HasPrefix(ev.eventSource, "integration-test-user (") {
					t.Errorf("EventSource = %q, want username+session prefix", ev.eventSource)
				}
				if d := ev.diff(t, "Label"); d["New"] != "Med Kits" || d["Old"] != nil {
					t.Errorf("Label diff = %v, want New=Med Kits Old=nil", d)
				}
				// The default_create_fn value is part of the recorded insert.
				if d := ev.diff(t, "Status"); d["New"] != "provisioned" {
					t.Errorf("Status diff = %v, want New=provisioned", d)
				}
				// PII is recorded unredacted (pinned; see file comment).
				if d := ev.diff(t, "InspectorBadge"); d["New"] != "INSP-99" {
					t.Errorf("InspectorBadge diff = %v, want New=INSP-99", d)
				}
			},
		},
		{
			name:       "update writes a diff event with old and new values",
			grants:     crateUpdateGrants,
			body:       fmt.Sprintf(`[{"op":"patch","path":"/supply-crates/%s","value":{"quantity":77}}]`, crateCoolant40ID),
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, _ []byte) {
				events := readChangeEvents(ctx, t, db, "SupplyCrates", crateCoolant40ID)
				if len(events) != 1 {
					t.Fatalf("event count = %d, want 1", len(events))
				}

				ev := events[0]
				if len(ev.changeSet) != 1 {
					t.Errorf("ChangeSet has %d entries, want only Quantity: %v", len(ev.changeSet), ev.changeSet)
				}
				if d := ev.diff(t, "Quantity"); d["Old"] != float64(40) || d["New"] != float64(77) {
					t.Errorf("Quantity diff = %v, want Old=40 New=77", d)
				}
			},
		},
		{
			name:       "update with no effective change is rejected",
			grants:     crateUpdateGrants,
			body:       fmt.Sprintf(`[{"op":"patch","path":"/supply-crates/%s","value":{"quantity":40}}]`, crateCoolant40ID),
			wantStatus: http.StatusBadRequest,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, _ []byte) {
				if events := readChangeEvents(ctx, t, db, "SupplyCrates", crateCoolant40ID); len(events) != 0 {
					t.Errorf("event count = %d, want 0 after rejected no-op update", len(events))
				}
			},
		},
		{
			name:       "delete writes an event recording the old values",
			grants:     grants{accesstypes.Delete: {supplyCratesResource}},
			body:       fmt.Sprintf(`[{"op":"remove","path":"/supply-crates/%s"}]`, crateCoolant40ID),
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, _ []byte) {
				events := readChangeEvents(ctx, t, db, "SupplyCrates", crateCoolant40ID)
				if len(events) != 1 {
					t.Fatalf("event count = %d, want 1", len(events))
				}

				ev := events[0]
				if d := ev.diff(t, "Label"); d["Old"] != "Coolant Cells" {
					t.Errorf("Label diff = %v, want Old=Coolant Cells", d)
				}
				// PII is recorded unredacted in delete events too (pinned).
				if d := ev.diff(t, "InspectorBadge"); d["Old"] != "INSP-77" {
					t.Errorf("InspectorBadge diff = %v, want Old=INSP-77", d)
				}
			},
		},
		{
			name: "untracked resource writes no events",
			grants: grants{accesstypes.Update: {
				shipsResource,
				fieldResource(shipsResource, "name"),
			}},
			body:       fmt.Sprintf(`[{"op":"patch","path":"/ships/%s","value":{"name":"Vanta II"}}]`, shipVantaID),
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, _ []byte) {
				if events := readChangeEvents(ctx, t, db, "Ships", shipVantaID); len(events) != 0 {
					t.Errorf("event count = %d, want 0 for untracked resource", len(events))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()

			db, err := prepareDatabase(ctx, t, "file://../schema/migrations", "file://testdata/seed")
			if err != nil {
				t.Fatal(err)
			}
			testApp := newTestApp(db, tt.grants)

			status, respBody := doRequest(t, testApp, http.MethodPatch, "/api/resources", tt.body)
			assertStatus(t, status, tt.wantStatus, respBody)
			if tt.verify != nil {
				tt.verify(ctx, t, db, respBody)
			}
		})
	}
}
