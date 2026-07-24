package integration

// This suite asserts permission behavior that is invariant across the planned migration
// of field permissions from fail open to fail closed. It only exercises fully tagged
// resources (Ships, CrewMembers) and RPC methods: every non-primary-key field carries an
// explicit perm tag, so the field-permission default is never consulted, and primary
// keys follow the resource-level grant by rule.
//
// These assertions must NOT be updated when the field-permission default changes. A
// failure here after changing the default means enforcement itself broke.

import (
	"fmt"
	"net/http"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"google.golang.org/grpc/codes"
)

const (
	shipsResource           = accesstypes.Resource("Ships")
	crewMembersResource     = accesstypes.Resource("CrewMembers")
	authorizeLaunchResource = accesstypes.Resource("AuthorizeLaunch")
)

func fieldResource(res accesstypes.Resource, tag string) accesstypes.Resource {
	return accesstypes.Resource(string(res) + "." + tag)
}

func TestInvariantQuery(t *testing.T) {
	// No t.Parallel(): all integration tests share one Spanner emulator instance, and
	// concurrent database creation/DDL across tests is unreliable on the emulator.
	ctx := t.Context()

	db, err := prepareDatabase(ctx, t)
	if err != nil {
		t.Fatal(err)
	}
	seedDatabase(ctx, t, db)

	allShipFieldsList := grants{accesstypes.List: {
		shipsResource,
		fieldResource(shipsResource, "registryCode"),
		fieldResource(shipsResource, "name"),
		fieldResource(shipsResource, "dockingBayId"),
		fieldResource(shipsResource, "cargoValue"),
		fieldResource(shipsResource, "updatedAt"),
	}}

	tests := []struct {
		name       string
		grants     grants
		target     string
		wantStatus int
		wantRows   int
		wantKeys   []string
	}{
		{
			name:       "list without resource grant is forbidden",
			grants:     nil,
			target:     "/api/ships",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "list with resource-only grant returns exactly the primary key",
			grants:     grants{accesstypes.List: {shipsResource}},
			target:     "/api/ships",
			wantStatus: http.StatusOK,
			wantRows:   2,
			wantKeys:   []string{"id"},
		},
		{
			name: "list with field grants returns exactly the granted fields",
			grants: grants{accesstypes.List: {
				shipsResource,
				fieldResource(shipsResource, "name"),
				fieldResource(shipsResource, "cargoValue"),
			}},
			target:     "/api/ships",
			wantStatus: http.StatusOK,
			wantRows:   2,
			wantKeys:   []string{"id", "name", "cargoValue"},
		},
		{
			name:       "list with all field grants returns all fields",
			grants:     allShipFieldsList,
			target:     "/api/ships",
			wantStatus: http.StatusOK,
			wantRows:   2,
			wantKeys:   []string{"id", "registryCode", "name", "dockingBayId", "cargoValue", "updatedAt"},
		},
		{
			name:       "requested column without field grant is forbidden",
			grants:     grants{accesstypes.List: {shipsResource}},
			target:     "/api/ships?columns=registryCode",
			wantStatus: http.StatusForbidden,
		},
		{
			name: "requested column with field grant returns exactly that column",
			grants: grants{accesstypes.List: {
				shipsResource,
				fieldResource(shipsResource, "name"),
			}},
			target:     "/api/ships?columns=name",
			wantStatus: http.StatusOK,
			wantRows:   2,
			wantKeys:   []string{"name"},
		},
		{
			name:       "read without resource grant is forbidden",
			grants:     grants{accesstypes.List: {shipsResource}}, // List grant does not satisfy Read
			target:     "/api/ships/" + shipVantaID,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "read with resource-only grant returns exactly the primary key",
			grants:     grants{accesstypes.Read: {shipsResource}},
			target:     "/api/ships/" + shipVantaID,
			wantStatus: http.StatusOK,
			wantKeys:   []string{"id"},
		},
		{
			name: "read with field grants returns exactly the granted fields",
			grants: grants{accesstypes.Read: {
				shipsResource,
				fieldResource(shipsResource, "name"),
			}},
			target:     "/api/ships/" + shipVantaID,
			wantStatus: http.StatusOK,
			wantKeys:   []string{"id", "name"},
		},
		{
			name:       "crew list with resource-only grant returns exactly the primary key",
			grants:     grants{accesstypes.List: {crewMembersResource}},
			target:     "/api/crew-members",
			wantStatus: http.StatusOK,
			wantRows:   1,
			wantKeys:   []string{"id"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			testApp := newTestApp(db, tt.grants)

			status, body := doRequest(t, testApp, http.MethodGet, tt.target, "")
			assertStatus(t, status, tt.wantStatus, body)
			if tt.wantStatus != http.StatusOK {
				return
			}

			if tt.wantRows > 0 {
				rows := decodeRows(t, body)
				if len(rows) != tt.wantRows {
					t.Fatalf("row count = %d, want %d: %s", len(rows), tt.wantRows, body)
				}
				for _, row := range rows {
					assertKeys(t, row, tt.wantKeys)
				}
			} else {
				assertKeys(t, decodeRow(t, body), tt.wantKeys)
			}
		})
	}
}

func TestInvariantMutation(t *testing.T) {
	// No t.Parallel(): all integration tests share one Spanner emulator instance, and
	// concurrent database creation/DDL across tests is unreliable on the emulator.
	ctx := t.Context()

	db, err := prepareDatabase(ctx, t)
	if err != nil {
		t.Fatal(err)
	}
	seedDatabase(ctx, t, db)

	createGrants := grants{accesstypes.Create: {
		shipsResource,
		fieldResource(shipsResource, "registryCode"),
		fieldResource(shipsResource, "name"),
		fieldResource(shipsResource, "cargoValue"),
	}}

	// The subtests below share one database and run sequentially: each case targets rows
	// that earlier cases do not modify.

	t.Run("create without resource grant is forbidden", func(t *testing.T) {
		testApp := newTestApp(db, nil)
		body := `[{"op":"add","path":"/ships","value":{"registryCode":"SSV-9001","name":"Nomad","cargoValue":10}}]`
		status, respBody := doRequest(t, testApp, http.MethodPatch, "/api/resources", body)
		assertStatus(t, status, http.StatusForbidden, respBody)
	})

	t.Run("create tagged field without field grant is forbidden", func(t *testing.T) {
		testApp := newTestApp(db, grants{accesstypes.Create: {shipsResource}})
		body := `[{"op":"add","path":"/ships","value":{"registryCode":"SSV-9002","name":"Nomad","cargoValue":10}}]`
		status, respBody := doRequest(t, testApp, http.MethodPatch, "/api/resources", body)
		assertStatus(t, status, http.StatusForbidden, respBody)
	})

	t.Run("create with field grants is allowed", func(t *testing.T) {
		testApp := newTestApp(db, createGrants)
		body := `[{"op":"add","path":"/ships","value":{"registryCode":"SSV-2001","name":"Kestrel","cargoValue":420000}}]`
		status, respBody := doRequest(t, testApp, http.MethodPatch, "/api/resources", body)
		assertStatus(t, status, http.StatusOK, respBody)

		resp := decodeRow(t, respBody)
		created, ok := resp["ships"].([]any)
		if !ok || len(created) != 1 {
			t.Fatalf("expected one created ship id, got: %s", respBody)
		}
		createdID, ok := created[0].(string)
		if !ok {
			t.Fatalf("created ship id is not a string: %s", respBody)
		}
		name := readColumn[string](ctx, t, db, "Ships", spanner.Key{createdID}, "Name")
		if name != "Kestrel" {
			t.Errorf("created ship Name = %q, want %q", name, "Kestrel")
		}
	})

	t.Run("update tagged field without field grant is forbidden", func(t *testing.T) {
		testApp := newTestApp(db, grants{accesstypes.Update: {shipsResource}})
		body := fmt.Sprintf(`[{"op":"patch","path":"/ships/%s","value":{"name":"Vanta II"}}]`, shipVantaID)
		status, respBody := doRequest(t, testApp, http.MethodPatch, "/api/resources", body)
		assertStatus(t, status, http.StatusForbidden, respBody)

		name := readColumn[string](ctx, t, db, "Ships", spanner.Key{shipVantaID}, "Name")
		if name != "Vanta" {
			t.Errorf("ship Name = %q, want unchanged %q", name, "Vanta")
		}
	})

	t.Run("update tagged field with field grant is allowed", func(t *testing.T) {
		testApp := newTestApp(db, grants{accesstypes.Update: {
			shipsResource,
			fieldResource(shipsResource, "name"),
		}})
		body := fmt.Sprintf(`[{"op":"patch","path":"/ships/%s","value":{"name":"Vanta II"}}]`, shipVantaID)
		status, respBody := doRequest(t, testApp, http.MethodPatch, "/api/resources", body)
		assertStatus(t, status, http.StatusOK, respBody)

		name := readColumn[string](ctx, t, db, "Ships", spanner.Key{shipVantaID}, "Name")
		if name != "Vanta II" {
			t.Errorf("ship Name = %q, want %q", name, "Vanta II")
		}
	})

	t.Run("update immutable field is rejected regardless of grants", func(t *testing.T) {
		testApp := newTestApp(db, grants{accesstypes.Update: {
			shipsResource,
			fieldResource(shipsResource, "registryCode"),
		}})
		body := fmt.Sprintf(`[{"op":"patch","path":"/ships/%s","value":{"registryCode":"SSV-0000"}}]`, shipVantaID)
		status, respBody := doRequest(t, testApp, http.MethodPatch, "/api/resources", body)
		assertStatus(t, status, http.StatusBadRequest, respBody)
	})

	t.Run("delete without resource grant is forbidden", func(t *testing.T) {
		testApp := newTestApp(db, grants{accesstypes.Update: {shipsResource}})
		body := fmt.Sprintf(`[{"op":"remove","path":"/ships/%s"}]`, shipComostID)
		status, respBody := doRequest(t, testApp, http.MethodPatch, "/api/resources", body)
		assertStatus(t, status, http.StatusForbidden, respBody)
	})

	t.Run("delete with resource grant is allowed", func(t *testing.T) {
		testApp := newTestApp(db, grants{accesstypes.Delete: {shipsResource}})
		body := fmt.Sprintf(`[{"op":"remove","path":"/ships/%s"}]`, shipComostID)
		status, respBody := doRequest(t, testApp, http.MethodPatch, "/api/resources", body)
		assertStatus(t, status, http.StatusOK, respBody)

		_, err := db.Single().ReadRow(ctx, "Ships", spanner.Key{shipComostID}, []string{"Name"})
		if spanner.ErrCode(err) != codes.NotFound {
			t.Errorf("expected ship to be deleted, ReadRow err = %v", err)
		}
	})
}

func TestInvariantRPC(t *testing.T) {
	// No t.Parallel(): all integration tests share one Spanner emulator instance, and
	// concurrent database creation/DDL across tests is unreliable on the emulator.
	ctx := t.Context()

	db, err := prepareDatabase(ctx, t)
	if err != nil {
		t.Fatal(err)
	}
	seedDatabase(ctx, t, db)

	tests := []struct {
		name       string
		grants     grants
		body       string
		wantStatus int
	}{
		{
			name:       "execute without grant is forbidden",
			grants:     nil,
			body:       fmt.Sprintf(`{"shipId":%q,"launchCode":"launch-7"}`, shipVantaID),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "execute with grant is allowed",
			grants:     grants{accesstypes.Execute: {authorizeLaunchResource}},
			body:       fmt.Sprintf(`{"shipId":%q,"launchCode":"launch-7"}`, shipVantaID),
			wantStatus: http.StatusOK,
		},
		{
			name:       "execute with grant still enforces request validation",
			grants:     grants{accesstypes.Execute: {authorizeLaunchResource}},
			body:       fmt.Sprintf(`{"shipId":%q,"launchCode":""}`, shipVantaID),
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			testApp := newTestApp(db, tt.grants)

			status, body := doRequest(t, testApp, http.MethodPost, "/api/authorize-launch", tt.body)
			assertStatus(t, status, tt.wantStatus, body)
		})
	}
}
