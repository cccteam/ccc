// Demonstrates: typescript.derived-object, typescript.imported-type, typescript.byte-slice.
package integration

// column_types_test: the columns typed by application types and by a byte slice.
// DistressCalls.Position is a GeoJSON Point whose TypeScript type the Go type declares
// with @typescript; the marshal files a call with a point, reads it back as the JSON it
// sent, and clears it with a null, while the seed's voice-relayed call carries none.
// MissionDocuments.Provenance is a plain struct: the upload records the origin, the
// generated Spanner methods store it, and the console and the portal both list it as
// one object under the field their grants name. MissionDocuments.Digest is a BYTES
// column: the upload records the file's SHA-256, and the row carries it as one base64
// string, the string the generated interface promises, never an array of numbers.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
)

func TestDistressCallPosition_importedType(t *testing.T) {
	t.Parallel()

	ctx, db, h := demoWorld(t)

	// The seed's fifth call was relayed by voice and names no point: the column is NULL
	// and the row carries the key with a null, as a genuine NULL does.
	const voiceCallID = "d0000000-0000-4000-8000-000000000005"
	status, body := doRequestAs(t, h, "marshal", http.MethodGet, sectorPath(anvil, "distress-calls/"+voiceCallID), "")
	assertStatus(t, status, http.StatusOK, body)
	if got, ok := decodeRow(t, body)["position"]; !ok || got != nil {
		t.Errorf("seeded voice call position = %v (present %v), want a null", got, ok)
	}

	// The marshal files a call with a point. The value is stored as the JSON it was sent
	// and read back as that JSON: a Point on the wire in both directions.
	point := map[string]any{"type": "Point", "coordinates": []any{-118.25, 34.05}}
	pointJSON, err := json.Marshal(point)
	if err != nil {
		t.Fatal(err)
	}
	status, body = doRequestAs(t, h, "marshal", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"add","path":%q,"value":{"summary":"Beacon fix on the outer lane","severity":2,"position":%s}}]`, opPath(anvil, "distress-calls"), pointJSON))
	assertStatus(t, status, http.StatusOK, body)
	ids, _ := decodeRow(t, body)["distressCalls"].([]any)
	if len(ids) != 1 {
		t.Fatalf("created ids = %v, want one call id: %s", ids, body)
	}
	id, _ := ids[0].(string)

	stored := readColumn[spanner.NullJSON](ctx, t, db, "DistressCalls", spanner.Key{id}, "Position")
	if !stored.Valid || !reflect.DeepEqual(stored.Value, point) {
		t.Errorf("Position column = %v, want the point %v", stored, point)
	}
	status, body = doRequestAs(t, h, "marshal", http.MethodGet, sectorPath(anvil, "distress-calls/"+id), "")
	assertStatus(t, status, http.StatusOK, body)
	if got := decodeRow(t, body)["position"]; !reflect.DeepEqual(got, point) {
		t.Errorf("position on read = %v, want the point %v", got, point)
	}

	// A null in a PATCH clears the column, and the row reads back null.
	status, body = doRequestAs(t, h, "marshal", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"position":null}}]`, opPath(anvil, "distress-calls/"+id)))
	assertStatus(t, status, http.StatusOK, body)
	if stored := readColumn[spanner.NullJSON](ctx, t, db, "DistressCalls", spanner.Key{id}, "Position"); stored.Valid {
		t.Errorf("Position column after the null = %v, want NULL", stored)
	}
	status, body = doRequestAs(t, h, "marshal", http.MethodGet, sectorPath(anvil, "distress-calls/"+id), "")
	assertStatus(t, status, http.StatusOK, body)
	if got, ok := decodeRow(t, body)["position"]; !ok || got != nil {
		t.Errorf("position after the null = %v (present %v), want a null", got, ok)
	}

	// The cadet's grants name no position: the field is masked, absent from the row.
	status, body = doRequestAs(t, h, "cadet", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"add","path":%q,"value":{"summary":"Cadet's own call","severity":1}}]`, opPath(anvil, "distress-calls")))
	assertStatus(t, status, http.StatusOK, body)
	cadetIDs, _ := decodeRow(t, body)["distressCalls"].([]any)
	cadetID, _ := cadetIDs[0].(string)
	status, body = doRequestAs(t, h, "cadet", http.MethodGet, sectorPath(anvil, "distress-calls/"+cadetID), "")
	assertStatus(t, status, http.StatusOK, body)
	if _, present := decodeRow(t, body)["position"]; present {
		t.Errorf("position is present on the cadet's read, whose grant does not name it: %s", body)
	}
}

func TestMissionDocumentProvenance_derivedObject(t *testing.T) {
	t.Parallel()

	h, _, _ := documentWorld(t)

	// The marshal attaches a brief to Halvard's stranded hauler: the upload records the
	// origin as one Provenance, stored by the generated Spanner methods.
	const haulerID = "80000000-0000-4000-8000-000000000001"
	body, contentType := multipartBody(t, fmt.Sprintf(`{"missionId":%q,"title":"Hauler brief"}`, haulerID),
		uploadFile{name: "brief.txt", contentType: "text/plain", content: []byte("Halvard hauler: crew of four")})
	status, respBody := doUploadAs(t, h, "marshal", sectorPath(anvil, "attach-mission-document"), body, contentType, false)
	assertStatus(t, status, http.StatusOK, respBody)

	wantOrigin := func(t *testing.T, row map[string]any, who string) {
		t.Helper()

		origin, ok := row["provenance"].(map[string]any)
		if !ok {
			t.Fatalf("%s: provenance = %v, want one object: %v", who, row["provenance"], row)
		}
		if origin["system"] != "console-upload" || origin["reference"] != "brief.txt" {
			t.Errorf("%s: provenance = %v, want system console-upload and reference brief.txt", who, origin)
		}
		if received, _ := origin["receivedAt"].(string); received == "" {
			t.Errorf("%s: provenance.receivedAt = %v, want a timestamp", who, origin["receivedAt"])
		}
		// The interface derived from the struct names exactly the tagged fields.
		var typed resources.Provenance
		raw, err := json.Marshal(origin)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &typed); err != nil || typed.System != "console-upload" {
			t.Errorf("%s: provenance %s does not read back as resources.Provenance: %v", who, raw, err)
		}
	}

	// The console lists the document with its origin under the marshal's grant.
	rows := readRows(t, h, "marshal", sectorPath(anvil, "mission-documents"))
	if len(rows) != 1 {
		t.Fatalf("rows = %v, want one", rows)
	}
	wantOrigin(t, rows[0], "marshal")

	// The portal, the second TypeScript target, lists the same object under the client's
	// grant, which names provenance beside the fields it always had.
	portalStatus, portalBody := doRequestAs(t, h, "client", http.MethodGet, "/portal/api/sectors/anvil/mission-documents", "")
	assertStatus(t, portalStatus, http.StatusOK, portalBody)
	portalRows := decodeRows(t, portalBody)
	if len(portalRows) != 1 {
		t.Fatalf("portal rows = %v, want one", portalRows)
	}
	wantOrigin(t, portalRows[0], "client")
}

func TestMissionDocumentDigest_byteSlice(t *testing.T) {
	t.Parallel()

	h, _, _ := documentWorld(t)

	// The marshal attaches a manifest to Halvard's stranded hauler: the body reads the
	// pending object back and records its SHA-256 in the BYTES column.
	const haulerID = "80000000-0000-4000-8000-000000000001"
	content := []byte("Halvard hauler: crew of four, one injured; hold formation at the belt edge.")
	body, contentType := multipartBody(t, fmt.Sprintf(`{"missionId":%q,"title":"Hauler manifest"}`, haulerID),
		uploadFile{name: "manifest.txt", contentType: "text/plain", content: content})
	status, respBody := doUploadAs(t, h, "marshal", sectorPath(anvil, "attach-mission-document"), body, contentType, false)
	assertStatus(t, status, http.StatusOK, respBody)

	rows := readRows(t, h, "marshal", sectorPath(anvil, "mission-documents"))
	if len(rows) != 1 {
		t.Fatalf("rows = %v, want one", rows)
	}

	// The wire carries the digest as one base64 string, as encoding/json writes a
	// []byte, and the generated interface types it string: never an array of numbers.
	sum := sha256.Sum256(content)
	digest, ok := rows[0]["digest"].(string)
	if !ok {
		t.Fatalf("digest = %v (%T), want one base64 string", rows[0]["digest"], rows[0]["digest"])
	}
	if want := base64.StdEncoding.EncodeToString(sum[:]); digest != want {
		t.Errorf("digest = %q, want %q, the SHA-256 of the uploaded bytes", digest, want)
	}

	// The same string reads back into the resource struct's []byte as the sum itself.
	raw, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatal(err)
	}
	var typed resources.MissionDocument
	if err := json.Unmarshal(raw, &typed); err != nil {
		t.Fatalf("row does not read back as resources.MissionDocument: %v", err)
	}
	if !reflect.DeepEqual(typed.Digest, sum[:]) {
		t.Errorf("Digest = %x, want %x", typed.Digest, sum[:])
	}
}
