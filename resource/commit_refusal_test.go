package resource

import (
	"net/http"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// recordedPatch is the least a buffered patch is: a resource and a patch type.
type recordedPatch struct {
	res accesstypes.Resource
	typ PatchType
}

func (p recordedPatch) Resource() accesstypes.Resource {
	return p.res
}

func (p recordedPatch) PatchType() PatchType {
	return p.typ
}

func (p recordedPatch) PrimaryKey() KeySet {
	return KeySet{}
}

// The ruled sentences, each named once so the tables below read as shapes.
const (
	hangarsDeleteSentence  = "Hangars: this record cannot be deleted while other records still reference it."
	shipsReferencedMissing = "Ships: a referenced record does not exist, or a value is too long for its field."
)

// TestBufferedPatches_referentialMessage pins the ruled wording of a referential refusal
// over what the transaction buffered: one sentence per shape, resources in first-buffered
// order, the change-event rows of a tracked resource never counted.
func TestBufferedPatches_referentialMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		patches []PatchSetMetadata
		want    string
	}{
		{
			name:    "deletes on one resource name the resource and the record",
			patches: []PatchSetMetadata{recordedPatch{"Hangars", DeletePatchType}},
			want:    hangarsDeleteSentence,
		},
		{
			name:    "several deletes of one resource are still one resource",
			patches: []PatchSetMetadata{recordedPatch{"Hangars", DeletePatchType}, recordedPatch{"Hangars", DeletePatchType}},
			want:    hangarsDeleteSentence,
		},
		{
			name:    "deletes on several resources list them in buffering order",
			patches: []PatchSetMetadata{recordedPatch{"Hangars", DeletePatchType}, recordedPatch{"Ships", DeletePatchType}, recordedPatch{"Hangars", DeletePatchType}},
			want:    "Hangars, Ships: a record cannot be deleted while other records still reference it.",
		},
		{
			name:    "a create names a referenced record that does not exist or a value too long",
			patches: []PatchSetMetadata{recordedPatch{"Ships", CreatePatchType}},
			want:    shipsReferencedMissing,
		},
		{
			name:    "an update is a write",
			patches: []PatchSetMetadata{recordedPatch{"Ships", UpdatePatchType}},
			want:    shipsReferencedMissing,
		},
		{
			name:    "a create-or-update is a write",
			patches: []PatchSetMetadata{recordedPatch{"Ships", CreateOrUpdatePatchType}},
			want:    shipsReferencedMissing,
		},
		{
			name:    "writes on several resources share the write sentence",
			patches: []PatchSetMetadata{recordedPatch{"Ships", CreatePatchType}, recordedPatch{"Missions", UpdatePatchType}},
			want:    "Ships, Missions: a referenced record does not exist, or a value is too long for its field.",
		},
		{
			name:    "deletes and writes together fall back to the sentence covering both causes",
			patches: []PatchSetMetadata{recordedPatch{"Hangars", DeletePatchType}, recordedPatch{"Ships", CreatePatchType}},
			want:    referentialRefusalFallback,
		},
		{
			name:    "a delete and an update on one resource are the mixed shape",
			patches: []PatchSetMetadata{recordedPatch{"Ships", DeletePatchType}, recordedPatch{"Ships", UpdatePatchType}},
			want:    referentialRefusalFallback,
		},
		{
			name:    "nothing buffered through the wrapper falls back to the same sentence",
			patches: nil,
			want:    referentialRefusalFallback,
		},
		{
			name:    "a tracked resource's change event is not a buffered patch",
			patches: []PatchSetMetadata{recordedPatch{"Ships", DeletePatchType}, &DataChangeEvent{TableName: "Ships"}},
			want:    "Ships: this record cannot be deleted while other records still reference it.",
		},
		{
			name:    "a tracked create names the resource, not its change event",
			patches: []PatchSetMetadata{recordedPatch{"Missions", CreatePatchType}, &DataChangeEvent{TableName: "Missions"}},
			want:    "Missions: a referenced record does not exist, or a value is too long for its field.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			buffered := newBufferedPatches()
			for _, p := range tt.patches {
				buffered.record(p)
			}

			if got := buffered.referentialMessage(); got != tt.want {
				t.Errorf("referentialMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTranslateCommitError pins that the translation keys on the gRPC code alone and
// words each code from the record's shape: FailedPrecondition, AlreadyExists, and a
// NotFound beside a create answer 409; OutOfRange answers 400; a NotFound over updates
// only answers 404; a record with nothing in it gets the code's covering sentence at the
// code's write status; the Spanner error stays the cause through any wrapping; and every
// other code, or an error that is no gRPC status at all, passes through as the same error
// with no client message.
func TestTranslateCommitError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		err         error
		patches     []PatchSetMetadata
		wantStatus  int
		wantMessage string
	}{
		// FailedPrecondition: the referential shapes, worded by referentialMessage.
		{
			name:        "FailedPrecondition over a delete answers 409 with the delete sentence",
			err:         status.Error(codes.FailedPrecondition, "Foreign key constraint `FK_Ships_HangarId` is violated on table `Ships`."),
			patches:     []PatchSetMetadata{recordedPatch{"Hangars", DeletePatchType}},
			wantStatus:  http.StatusConflict,
			wantMessage: hangarsDeleteSentence,
		},
		{
			name:        "FailedPrecondition over a create answers 409 with the broadened write sentence",
			err:         status.Error(codes.FailedPrecondition, "New value exceeds the maximum size limit for this column"),
			patches:     []PatchSetMetadata{recordedPatch{"Ships", CreatePatchType}},
			wantStatus:  http.StatusConflict,
			wantMessage: shipsReferencedMissing,
		},
		{
			name:        "the code reads through go-playground wrapping",
			err:         errors.Wrap(errors.Wrap(status.Error(codes.FailedPrecondition, "refused"), "inner"), "outer"),
			patches:     []PatchSetMetadata{recordedPatch{"Hangars", DeletePatchType}},
			wantStatus:  http.StatusConflict,
			wantMessage: hangarsDeleteSentence,
		},
		// AlreadyExists: a create cannot tell a taken key from a duplicate unique value.
		{
			name:        "AlreadyExists over a create answers 409 naming the key or a unique value",
			err:         status.Error(codes.AlreadyExists, "Row [a0…, 2] in table RefitTasks already exists"),
			patches:     []PatchSetMetadata{recordedPatch{"RefitTasks", CreatePatchType}},
			wantStatus:  http.StatusConflict,
			wantMessage: "RefitTasks: a record with this key or a unique value already exists.",
		},
		{
			name:        "AlreadyExists over a create-or-update is the create shape",
			err:         status.Error(codes.AlreadyExists, "Unique index violation on index ShipsByRegistry"),
			patches:     []PatchSetMetadata{recordedPatch{"Ships", CreateOrUpdatePatchType}},
			wantStatus:  http.StatusConflict,
			wantMessage: "Ships: a record with this key or a unique value already exists.",
		},
		{
			name:        "AlreadyExists over updates only answers 409 naming another record's unique value",
			err:         status.Error(codes.AlreadyExists, "Unique index violation on index ClientsByName"),
			patches:     []PatchSetMetadata{recordedPatch{"Clients", UpdatePatchType}},
			wantStatus:  http.StatusConflict,
			wantMessage: "Clients: a unique value already exists on another record.",
		},
		{
			name:        "AlreadyExists over an update and a create on two resources is the create shape in buffering order",
			err:         status.Error(codes.AlreadyExists, "already exists"),
			patches:     []PatchSetMetadata{recordedPatch{"Clients", UpdatePatchType}, recordedPatch{"Ships", CreatePatchType}},
			wantStatus:  http.StatusConflict,
			wantMessage: "Clients, Ships: a record with this key or a unique value already exists.",
		},
		{
			name:        "AlreadyExists ignores deletes buffered beside the writes",
			err:         status.Error(codes.AlreadyExists, "already exists"),
			patches:     []PatchSetMetadata{recordedPatch{"Sorties", DeletePatchType}, recordedPatch{"Clients", UpdatePatchType}},
			wantStatus:  http.StatusConflict,
			wantMessage: "Clients: a unique value already exists on another record.",
		},
		{
			name:        "AlreadyExists over nothing buffered answers 409 with the covering sentence",
			err:         status.Error(codes.AlreadyExists, "already exists"),
			wantStatus:  http.StatusConflict,
			wantMessage: alreadyExistsFallback,
		},
		// OutOfRange: a CHECK constraint, 400 for any write.
		{
			name:        "OutOfRange over an update answers 400 naming the range",
			err:         status.Error(codes.OutOfRange, "Check constraint `Missions`.`CK_Missions_Hazard` is violated."),
			patches:     []PatchSetMetadata{recordedPatch{"Missions", UpdatePatchType}},
			wantStatus:  http.StatusBadRequest,
			wantMessage: "Missions: a value is outside the range the record allows.",
		},
		{
			name:        "OutOfRange over a create answers 400 with the same sentence",
			err:         status.Error(codes.OutOfRange, "Check constraint violated"),
			patches:     []PatchSetMetadata{recordedPatch{"Missions", CreatePatchType}, &DataChangeEvent{TableName: "Missions"}},
			wantStatus:  http.StatusBadRequest,
			wantMessage: "Missions: a value is outside the range the record allows.",
		},
		{
			name:        "OutOfRange over nothing buffered answers 400 with the covering sentence",
			err:         status.Error(codes.OutOfRange, "Check constraint violated"),
			wantStatus:  http.StatusBadRequest,
			wantMessage: outOfRangeFallback,
		},
		// NotFound: updates only means the row is missing; a create means its parent is.
		{
			name:        "NotFound over updates only answers 404 for the record",
			err:         status.Error(codes.NotFound, "Row [missing] in table Clients is missing. Row cannot be updated."),
			patches:     []PatchSetMetadata{recordedPatch{"Clients", UpdatePatchType}},
			wantStatus:  http.StatusNotFound,
			wantMessage: "Clients: this record does not exist.",
		},
		{
			name:        "NotFound over updates on two resources lists both in buffering order",
			err:         status.Error(codes.NotFound, "missing"),
			patches:     []PatchSetMetadata{recordedPatch{"Clients", UpdatePatchType}, recordedPatch{"Pilots", UpdatePatchType}},
			wantStatus:  http.StatusNotFound,
			wantMessage: "Clients, Pilots: this record does not exist.",
		},
		{
			name:        "NotFound over a create answers 409 for the referenced parent",
			err:         status.Error(codes.NotFound, "Parent row for row [p404, n9] in table Nested is missing."),
			patches:     []PatchSetMetadata{recordedPatch{"RefitTasks", CreatePatchType}},
			wantStatus:  http.StatusConflict,
			wantMessage: "RefitTasks: a referenced record does not exist.",
		},
		{
			name:        "NotFound over an update beside a create is the create shape",
			err:         status.Error(codes.NotFound, "missing"),
			patches:     []PatchSetMetadata{recordedPatch{"Refits", UpdatePatchType}, recordedPatch{"RefitTasks", CreateOrUpdatePatchType}},
			wantStatus:  http.StatusConflict,
			wantMessage: "Refits, RefitTasks: a referenced record does not exist.",
		},
		{
			name:        "NotFound ignores deletes buffered beside the update",
			err:         status.Error(codes.NotFound, "missing"),
			patches:     []PatchSetMetadata{recordedPatch{"Clients", UpdatePatchType}, recordedPatch{"Sorties", DeletePatchType}},
			wantStatus:  http.StatusNotFound,
			wantMessage: "Clients: this record does not exist.",
		},
		{
			name:        "NotFound over nothing buffered answers 409 with the covering sentence",
			err:         status.Error(codes.NotFound, "missing"),
			wantStatus:  http.StatusConflict,
			wantMessage: notFoundFallback,
		},
		// Everything else passes through.
		{
			name:    "InvalidArgument passes through",
			err:     status.Error(codes.InvalidArgument, "NUMERIC value exceeds the precision"),
			patches: []PatchSetMetadata{recordedPatch{"Missions", CreatePatchType}},
		},
		{
			name:    "Internal passes through",
			err:     status.Error(codes.Internal, "string field contains invalid UTF-8"),
			patches: []PatchSetMetadata{recordedPatch{"Missions", CreatePatchType}},
		},
		{
			name:    "Aborted passes through",
			err:     status.Error(codes.Aborted, "Transaction was aborted."),
			patches: []PatchSetMetadata{recordedPatch{"Hangars", DeletePatchType}},
		},
		{
			name:    "an unknown code passes through",
			err:     status.Error(codes.Code(1000), "no such code"),
			patches: []PatchSetMetadata{recordedPatch{"Hangars", DeletePatchType}},
		},
		{
			name:    "an error that is no gRPC status passes through",
			err:     errors.New("the function's own failure"),
			patches: []PatchSetMetadata{recordedPatch{"Hangars", DeletePatchType}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			buffered := newBufferedPatches()
			for _, p := range tt.patches {
				buffered.record(p)
			}

			got := translateCommitError(tt.err, buffered)
			if tt.wantStatus == 0 {
				if !errors.Is(got, tt.err) || httpio.HasClientMessage(got) {
					t.Fatalf("translateCommitError() = %v, want the error passed through with no client message", got)
				}

				return
			}

			if gotStatus := clientStatus(got); gotStatus != tt.wantStatus {
				t.Fatalf("translateCommitError() = %v, answers %d, want %d", got, gotStatus, tt.wantStatus)
			}
			if msg := httpio.Message(got); msg != tt.wantMessage {
				t.Errorf("httpio.Message() = %q, want %q", msg, tt.wantMessage)
			}
			if !errors.Is(got, tt.err) {
				t.Errorf("the Spanner error is not the cause of the client message: %v", got)
			}
		})
	}
}

// clientStatus reads the status an error's client message answers with, 0 when it has
// none of the statuses the translation can produce.
func clientStatus(err error) int {
	switch {
	case httpio.HasConflict(err):
		return http.StatusConflict
	case httpio.HasBadRequest(err):
		return http.StatusBadRequest
	case httpio.HasNotFound(err):
		return http.StatusNotFound
	default:
		return 0
	}
}
