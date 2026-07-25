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
	"context"
	"fmt"
	"net/http"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	initiator "github.com/cccteam/db-initiator"
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
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, "file://../schema/migrations", "file://testdata/seed")
	if err != nil {
		t.Fatal(err)
	}

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
	t.Parallel()

	createGrants := grants{accesstypes.Create: {
		shipsResource,
		fieldResource(shipsResource, "registryCode"),
		fieldResource(shipsResource, "name"),
		fieldResource(shipsResource, "cargoValue"),
	}}

	crewCreateGrants := grants{accesstypes.Create: {
		crewMembersResource,
		fieldResource(crewMembersResource, "shipId"),
		fieldResource(crewMembersResource, "name"),
		fieldResource(crewMembersResource, "rank"),
		fieldResource(crewMembersResource, "clearanceLevel"),
	}}

	crewCreateBody := fmt.Sprintf(`[{"op":"add","path":"/","value":{"shipId":%q,"name":"Torvald Hess","rank":"Loadmaster","clearanceLevel":1}}]`, shipVantaID)

	// Each case prepares its own seeded database, fully isolating the cases from one
	// another.
	//
	// Ships mutations flow through the consolidated PATCH /api/resources endpoint.
	// CrewMembers is excluded from handler consolidation, so its mutations flow through
	// the standalone PATCH /api/crew-members endpoint; those cases assert that the
	// standalone mutation surface enforces the same permission contract as the
	// consolidated one.
	tests := []struct {
		name       string
		grants     grants
		target     string
		body       string
		wantStatus int
		verify     func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, respBody []byte)
	}{
		{
			name:       "create without resource grant is forbidden",
			grants:     nil,
			target:     "/api/resources",
			body:       `[{"op":"add","path":"/ships","value":{"registryCode":"SSV-9001","name":"Nomad","cargoValue":10}}]`,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "create tagged field without field grant is forbidden",
			grants:     grants{accesstypes.Create: {shipsResource}},
			target:     "/api/resources",
			body:       `[{"op":"add","path":"/ships","value":{"registryCode":"SSV-9002","name":"Nomad","cargoValue":10}}]`,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "create with field grants is allowed",
			grants:     createGrants,
			target:     "/api/resources",
			body:       `[{"op":"add","path":"/ships","value":{"registryCode":"SSV-2001","name":"Kestrel","cargoValue":420000}}]`,
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, respBody []byte) {
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
			},
		},
		{
			name:       "update tagged field without field grant is forbidden",
			grants:     grants{accesstypes.Update: {shipsResource}},
			target:     "/api/resources",
			body:       fmt.Sprintf(`[{"op":"patch","path":"/ships/%s","value":{"name":"Vanta II"}}]`, shipVantaID),
			wantStatus: http.StatusForbidden,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, _ []byte) {
				name := readColumn[string](ctx, t, db, "Ships", spanner.Key{shipVantaID}, "Name")
				if name != "Vanta" {
					t.Errorf("ship Name = %q, want unchanged %q", name, "Vanta")
				}
			},
		},
		{
			name: "update tagged field with field grant is allowed",
			grants: grants{accesstypes.Update: {
				shipsResource,
				fieldResource(shipsResource, "name"),
			}},
			target:     "/api/resources",
			body:       fmt.Sprintf(`[{"op":"patch","path":"/ships/%s","value":{"name":"Vanta II"}}]`, shipVantaID),
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, _ []byte) {
				name := readColumn[string](ctx, t, db, "Ships", spanner.Key{shipVantaID}, "Name")
				if name != "Vanta II" {
					t.Errorf("ship Name = %q, want %q", name, "Vanta II")
				}
			},
		},
		{
			name: "update immutable field is rejected regardless of grants",
			grants: grants{accesstypes.Update: {
				shipsResource,
				fieldResource(shipsResource, "registryCode"),
			}},
			target:     "/api/resources",
			body:       fmt.Sprintf(`[{"op":"patch","path":"/ships/%s","value":{"registryCode":"SSV-0000"}}]`, shipVantaID),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "delete without resource grant is forbidden",
			grants:     grants{accesstypes.Update: {shipsResource}},
			target:     "/api/resources",
			body:       fmt.Sprintf(`[{"op":"remove","path":"/ships/%s"}]`, shipComostID),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "delete with resource grant is allowed",
			grants:     grants{accesstypes.Delete: {shipsResource}},
			target:     "/api/resources",
			body:       fmt.Sprintf(`[{"op":"remove","path":"/ships/%s"}]`, shipComostID),
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, _ []byte) {
				_, err := db.Single().ReadRow(ctx, "Ships", spanner.Key{shipComostID}, []string{"Name"})
				if spanner.ErrCode(err) != codes.NotFound {
					t.Errorf("expected ship to be deleted, ReadRow err = %v", err)
				}
			},
		},
		{
			name:       "standalone create without resource grant is forbidden",
			grants:     nil,
			target:     "/api/crew-members",
			body:       crewCreateBody,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "standalone create tagged field without field grant is forbidden",
			grants:     grants{accesstypes.Create: {crewMembersResource}},
			target:     "/api/crew-members",
			body:       crewCreateBody,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "standalone create with field grants is allowed",
			grants:     crewCreateGrants,
			target:     "/api/crew-members",
			body:       crewCreateBody,
			wantStatus: http.StatusOK,
		},
		{
			name:       "standalone update tagged field without field grant is forbidden",
			grants:     grants{accesstypes.Update: {crewMembersResource}},
			target:     "/api/crew-members",
			body:       fmt.Sprintf(`[{"op":"patch","path":"/%s","value":{"rank":"Captain"}}]`, crewIlyanID),
			wantStatus: http.StatusForbidden,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, _ []byte) {
				rank := readColumn[string](ctx, t, db, "CrewMembers", spanner.Key{crewIlyanID}, "Rank")
				if rank != "Navigator" {
					t.Errorf("crew member Rank = %q, want unchanged %q", rank, "Navigator")
				}
			},
		},
		{
			name: "standalone update tagged field with field grant is allowed",
			grants: grants{accesstypes.Update: {
				crewMembersResource,
				fieldResource(crewMembersResource, "rank"),
			}},
			target:     "/api/crew-members",
			body:       fmt.Sprintf(`[{"op":"patch","path":"/%s","value":{"rank":"Captain"}}]`, crewIlyanID),
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, _ []byte) {
				rank := readColumn[string](ctx, t, db, "CrewMembers", spanner.Key{crewIlyanID}, "Rank")
				if rank != "Captain" {
					t.Errorf("crew member Rank = %q, want %q", rank, "Captain")
				}
			},
		},
		{
			name:       "standalone delete without resource grant is forbidden",
			grants:     grants{accesstypes.Update: {crewMembersResource}},
			target:     "/api/crew-members",
			body:       fmt.Sprintf(`[{"op":"remove","path":"/%s"}]`, crewIlyanID),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "standalone delete with resource grant is allowed",
			grants:     grants{accesstypes.Delete: {crewMembersResource}},
			target:     "/api/crew-members",
			body:       fmt.Sprintf(`[{"op":"remove","path":"/%s"}]`, crewIlyanID),
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, _ []byte) {
				_, err := db.Single().ReadRow(ctx, "CrewMembers", spanner.Key{crewIlyanID}, []string{"Name"})
				if spanner.ErrCode(err) != codes.NotFound {
					t.Errorf("expected crew member to be deleted, ReadRow err = %v", err)
				}
			},
		},
		{
			name:       "non-consolidated resource is not reachable through the consolidated route",
			grants:     crewCreateGrants,
			target:     "/api/resources",
			body:       fmt.Sprintf(`[{"op":"add","path":"/crew-members","value":{"shipId":%q,"name":"Torvald Hess","rank":"Loadmaster","clearanceLevel":1}}]`, shipVantaID),
			wantStatus: http.StatusBadRequest,
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

			status, respBody := doRequest(t, testApp, http.MethodPatch, tt.target, tt.body)
			assertStatus(t, status, tt.wantStatus, respBody)
			if tt.verify != nil {
				tt.verify(ctx, t, db, respBody)
			}
		})
	}
}

func TestInvariantRPC(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, "file://../schema/migrations", "file://testdata/seed")
	if err != nil {
		t.Fatal(err)
	}

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
