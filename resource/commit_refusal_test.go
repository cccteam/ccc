package resource

import (
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

// TestBufferedPatches_refusalMessage pins the ruled wording of a referential refusal
// over what the transaction buffered: one sentence per shape, resources in first-buffered
// order, the change-event rows of a tracked resource never counted.
func TestBufferedPatches_refusalMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		patches []PatchSetMetadata
		want    string
	}{
		{
			name:    "deletes on one resource name the resource and the record",
			patches: []PatchSetMetadata{recordedPatch{"Hangars", DeletePatchType}},
			want:    "Hangars: this record cannot be deleted while other records still reference it.",
		},
		{
			name:    "several deletes of one resource are still one resource",
			patches: []PatchSetMetadata{recordedPatch{"Hangars", DeletePatchType}, recordedPatch{"Hangars", DeletePatchType}},
			want:    "Hangars: this record cannot be deleted while other records still reference it.",
		},
		{
			name:    "deletes on several resources list them in buffering order",
			patches: []PatchSetMetadata{recordedPatch{"Hangars", DeletePatchType}, recordedPatch{"Ships", DeletePatchType}, recordedPatch{"Hangars", DeletePatchType}},
			want:    "Hangars, Ships: a record cannot be deleted while other records still reference it.",
		},
		{
			name:    "a create names a referenced record that does not exist",
			patches: []PatchSetMetadata{recordedPatch{"Ships", CreatePatchType}},
			want:    "Ships: a referenced record does not exist.",
		},
		{
			name:    "an update is a write",
			patches: []PatchSetMetadata{recordedPatch{"Ships", UpdatePatchType}},
			want:    "Ships: a referenced record does not exist.",
		},
		{
			name:    "a create-or-update is a write",
			patches: []PatchSetMetadata{recordedPatch{"Ships", CreateOrUpdatePatchType}},
			want:    "Ships: a referenced record does not exist.",
		},
		{
			name:    "writes on several resources share the write sentence",
			patches: []PatchSetMetadata{recordedPatch{"Ships", CreatePatchType}, recordedPatch{"Missions", UpdatePatchType}},
			want:    "Ships, Missions: a referenced record does not exist.",
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
			want:    "Missions: a referenced record does not exist.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			buffered := newBufferedPatches()
			for _, p := range tt.patches {
				buffered.record(p)
			}

			if got := buffered.refusalMessage(); got != tt.want {
				t.Errorf("refusalMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTranslateCommitError pins that the translation keys on the gRPC code alone: a
// FailedPrecondition becomes a 409 carrying the composed message with the Spanner error
// kept as its cause, through any wrapping; every other code, and an error that is not a
// gRPC status at all, passes through as the same error.
func TestTranslateCommitError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		err         error
		wantMessage string
	}{
		{
			name:        "FailedPrecondition answers 409 with the composed message",
			err:         status.Error(codes.FailedPrecondition, "Foreign key constraint `FK_Ships_HangarId` is violated on table `Ships`."),
			wantMessage: "Hangars: this record cannot be deleted while other records still reference it.",
		},
		{
			name:        "the code reads through go-playground wrapping",
			err:         errors.Wrap(errors.Wrap(status.Error(codes.FailedPrecondition, "refused"), "inner"), "outer"),
			wantMessage: "Hangars: this record cannot be deleted while other records still reference it.",
		},
		{
			name: "AlreadyExists passes through",
			err:  status.Error(codes.AlreadyExists, "Row [p4] in table Parents already exists"),
		},
		{
			name: "OutOfRange passes through",
			err:  status.Error(codes.OutOfRange, "Check constraint `CK_Children_Qty` is violated"),
		},
		{
			name: "NotFound passes through",
			err:  status.Error(codes.NotFound, "Row [missing] in table Parents is missing"),
		},
		{
			name: "Aborted passes through",
			err:  status.Error(codes.Aborted, "Transaction was aborted."),
		},
		{
			name: "an error that is no gRPC status passes through",
			err:  errors.New("the function's own failure"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			buffered := newBufferedPatches()
			buffered.record(recordedPatch{"Hangars", DeletePatchType})

			got := translateCommitError(tt.err, buffered)
			if tt.wantMessage == "" {
				if !errors.Is(got, tt.err) || httpio.HasClientMessage(got) {
					t.Fatalf("translateCommitError() = %v, want the error passed through with no client message", got)
				}

				return
			}

			if !httpio.HasConflict(got) {
				t.Fatalf("translateCommitError() = %v, want a 409 conflict", got)
			}
			if msg := httpio.Message(got); msg != tt.wantMessage {
				t.Errorf("httpio.Message() = %q, want %q", msg, tt.wantMessage)
			}
			if !errors.Is(got, tt.err) {
				t.Errorf("the Spanner error is not the cause of the conflict: %v", got)
			}
		})
	}
}
