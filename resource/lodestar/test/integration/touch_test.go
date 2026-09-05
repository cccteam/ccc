package integration

// This suite pins the first-class Touch: HailShip fires the generated NewShipTouch,
// so the full update pipeline runs with no caller-set fields — UpdatedAt takes the
// commit timestamp and the ship's log records who hailed with every field unchanged.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	initiator "github.com/cccteam/db-initiator"
	"google.golang.org/api/iterator"
)

// latestChangeEvent reads the newest DataChangeEvents row for one tracked row.
func latestChangeEvent(t *testing.T, db *initiator.SpannerDB, tableName, rowID string) (eventSource string, changeSetKeys []string) {
	t.Helper()

	iter := db.Single().Query(t.Context(), spanner.Statement{
		SQL: `SELECT EventSource, TO_JSON_STRING(ChangeSet)
		        FROM DataChangeEvents
		       WHERE TableName = @tableName AND RowId = @rowID
		    ORDER BY EventTime DESC, Sequence DESC
		       LIMIT 1`,
		Params: map[string]any{"tableName": tableName, "rowID": rowID},
	})
	defer iter.Stop()

	row, err := iter.Next()
	if errors.Is(err, iterator.Done) {
		t.Fatalf("no DataChangeEvents row for %s %s", tableName, rowID)
	}
	if err != nil {
		t.Fatalf("DataChangeEvents query: %v", err)
	}

	var changeSetJSON string
	if err := row.Columns(&eventSource, &changeSetJSON); err != nil {
		t.Fatalf("row.Columns: %v", err)
	}

	changeSet := map[string]any{}
	if err := json.Unmarshal([]byte(changeSetJSON), &changeSet); err != nil {
		t.Fatalf("ChangeSet unmarshal: %v: %s", err, changeSetJSON)
	}
	for key := range changeSet {
		changeSetKeys = append(changeSetKeys, key)
	}

	return eventSource, changeSetKeys
}

func TestHailShipTouch(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestApp(db, grants{accesstypes.Execute: {accesstypes.Resource("HailShip")}})

	// Seed rows never carry the stamp: UpdatedAt is written only by the update pipeline.
	if got := readColumn[spanner.NullTime](ctx, t, db, "Ships", spanner.Key{shipKingfisherID}, "UpdatedAt"); got.Valid {
		t.Fatalf("seeded UpdatedAt = %v, want unset", got)
	}

	status, body := doRequestAs(t, h, "hailer-hal", http.MethodPost, sectorPath(anvil, "hail-ship"), fmt.Sprintf(`{"shipId":%q}`, shipKingfisherID))
	assertStatus(t, status, http.StatusOK, body)

	first := readColumn[spanner.NullTime](ctx, t, db, "Ships", spanner.Key{shipKingfisherID}, "UpdatedAt")
	if !first.Valid {
		t.Fatal("UpdatedAt not stamped by the touch")
	}

	eventSource, changeSetKeys := latestChangeEvent(t, db, "Ships", shipKingfisherID)
	if want := "hailer-hal ("; !strings.HasPrefix(eventSource, want) {
		t.Errorf("eventSource = %q, want the session-derived %q prefix", eventSource, want)
	}
	if len(changeSetKeys) != 1 || changeSetKeys[0] != "UpdatedAt" {
		t.Errorf("changeSet keys = %v, want exactly [UpdatedAt]", changeSetKeys)
	}

	status, body = doRequestAs(t, h, "hailer-hal", http.MethodPost, sectorPath(anvil, "hail-ship"), fmt.Sprintf(`{"shipId":%q}`, shipKingfisherID))
	assertStatus(t, status, http.StatusOK, body)
	second := readColumn[spanner.NullTime](ctx, t, db, "Ships", spanner.Key{shipKingfisherID}, "UpdatedAt")
	if !second.Valid || !second.Time.After(first.Time) {
		t.Errorf("second touch UpdatedAt = %v, want after the first %v", second, first)
	}

	// The generated frame locates the @target row before the body runs.
	status, body = doRequestAs(t, h, "hailer-hal", http.MethodPost, sectorPath(anvil, "hail-ship"), `{"shipId":"70000000-0000-4000-8000-00000000dead"}`)
	assertStatus(t, status, http.StatusNotFound, body)
}
