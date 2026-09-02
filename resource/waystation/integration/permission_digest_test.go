package integration

// Bootstrap parity for the permission digest endpoint (design plan §13): the
// shipped personas fetch their structural grant enumeration through the real
// engine — the same channel the frontend renders navigation and forms from.
// The digest is advisory and non-folding, so these pins are pure functions of
// the shipped role config: they move only when demo_access.json does.

import (
	"encoding/json"
	"maps"
	"net/http"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

func TestPermissionDigest(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	h := newDemoApp(ctx, t, db)

	tests := []struct {
		name   string
		user   accesstypes.User
		target string
		// wantEntries are exact per-resource pins; resources not listed here
		// are unasserted unless named in absentKeys.
		wantEntries accesstypes.PermissionDigest
		absentKeys  []accesstypes.Resource
		wantEmpty   bool
	}{
		{
			// The domain digest reflects the Foreman role's grant structure:
			// unconditional grants report granted, condition-limited ones
			// conditional (nothing folds — even new.-referencing create
			// conditions surface structurally), and field entries carry the
			// same tri-state a create form renders inputs from.
			name:   "foreman's station digest carries the tri-state per grant",
			user:   "foreman-okafor",
			target: "/api/permission-digest?domain=ws-alpha",
			wantEntries: accesstypes.PermissionDigest{
				"WorkOrders": {
					"List":   accesstypes.DigestGranted,
					"Read":   accesstypes.DigestGranted,
					"Create": accesstypes.DigestConditional, // limited by: new.priority <= 3
					"Update": accesstypes.DigestConditional, // limited by: state IN ('draft', 'scheduled')
				},
				"WorkOrders.title": {
					"List":   accesstypes.DigestGranted,
					"Read":   accesstypes.DigestGranted,
					"Create": accesstypes.DigestConditional,
				},
				"WorkOrders.priority": {
					"List":   accesstypes.DigestGranted,
					"Read":   accesstypes.DigestGranted,
					"Create": accesstypes.DigestConditional,
					"Update": accesstypes.DigestConditional,
				},
				"Requisitions": {
					"List":   accesstypes.DigestConditional, // limited by: requestedBy = subject
					"Read":   accesstypes.DigestConditional,
					"Create": accesstypes.DigestGranted,
				},
				"RequisitionLines": {
					"List":   accesstypes.DigestGranted,
					"Read":   accesstypes.DigestGranted,
					"Create": accesstypes.DigestConditional, // limited by: state = 'draft'
					"Update": accesstypes.DigestConditional,
					"Delete": accesstypes.DigestConditional,
				},
				"SubmitRequisition": {
					"Execute": accesstypes.DigestGranted,
				},
			},
			// Global-scope resources never leak into a domain digest, and
			// resources other personas hold stay absent — denial is absence.
			absentKeys: []accesstypes.Resource{"Suppliers", "CatalogItems", "IncidentReports"},
		},
		{
			// The same session's global digest is a different scope: the
			// request's input selects the partition, never payload structure.
			name:   "foreman's global digest is the global roles' structure",
			user:   "foreman-okafor",
			target: "/api/permission-digest",
			wantEntries: accesstypes.PermissionDigest{
				"Waystations":    {"List": accesstypes.DigestGranted, "Read": accesstypes.DigestGranted},
				"CatalogItems":   {"List": accesstypes.DigestGranted, "Read": accesstypes.DigestGranted},
				"Suppliers":      {"List": accesstypes.DigestConditional}, // limited by: active = true
				"Suppliers.name": {"List": accesstypes.DigestConditional},
			},
			absentKeys: []accesstypes.Resource{"WorkOrders", "Requisitions"},
		},
		{
			// No roles at ws-beta: the digest is empty — consistent with the
			// concealed posture, this endpoint never confirms tenant existence.
			name:      "a station without a foothold digests to nothing",
			user:      "foreman-okafor",
			target:    "/api/permission-digest?domain=ws-beta",
			wantEmpty: true,
		},
		{
			name:      "an unknown domain digests to nothing, indistinguishably",
			user:      "foreman-okafor",
			target:    "/api/permission-digest?domain=ws-phantom",
			wantEmpty: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, http.MethodGet, tt.target, "")
			if status != http.StatusOK {
				t.Fatalf("GET %s status = %d, want %d (body %s)", tt.target, status, http.StatusOK, body)
			}

			var got accesstypes.PermissionDigest
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatalf("unmarshaling digest %s: %v", body, err)
			}

			if tt.wantEmpty && len(got) != 0 {
				t.Errorf("digest = %v, want empty", got)
			}
			for res, want := range tt.wantEntries {
				if !maps.Equal(want, got[res]) {
					t.Errorf("digest[%s] = %v, want %v", res, got[res], want)
				}
			}
			for _, res := range tt.absentKeys {
				if _, ok := got[res]; ok {
					t.Errorf("digest carries %s = %v, want absent (denied is absence)", res, got[res])
				}
			}
		})
	}
}
