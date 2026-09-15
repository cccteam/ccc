package resource

import (
	"strings"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"google.golang.org/grpc/codes"
)

// The sentences for a refusal whose cause the record cannot tell apart, one per gRPC code:
// for a referential refusal, deletes and writes buffered together or nothing buffered at
// all; for the other codes, nothing buffered through the wrapper. Each covers every cause
// its code carries.
const (
	referentialRefusalFallback = "The request could not be applied: a deleted record is still referenced, or a referenced record does not exist."
	alreadyExistsFallback      = "The request could not be applied: a record with this key or a unique value already exists."
	outOfRangeFallback         = "The request could not be applied: a value is outside the range the record allows."
	notFoundFallback           = "The request could not be applied: a record to update does not exist, or a referenced record does not exist."
)

// bufferedPatches is the record a write transaction keeps of what it buffered: every
// resource in first-buffered order, each with the patch types buffered for it. When
// Spanner refuses the commit, the record is all the library knows about what the refusal
// concerns, and the client message is composed from it alone: the resource names the
// caller addressed, never the referencing table, the constraint, the index, the key, the
// value, or a count.
//
// The change-event rows a tracked resource writes beside its patches are not recorded:
// they are the library's own bookkeeping, never a resource the caller addressed, and
// counting them would turn every tracked delete into the mixed shape.
type bufferedPatches struct {
	order []accesstypes.Resource
	types map[accesstypes.Resource]map[PatchType]bool
}

// newBufferedPatches returns an empty record.
func newBufferedPatches() *bufferedPatches {
	return &bufferedPatches{
		types: make(map[accesstypes.Resource]map[PatchType]bool),
	}
}

// record notes one buffered patch by its resource and patch type.
func (b *bufferedPatches) record(patch PatchSetMetadata) {
	if _, isEvent := patch.(*DataChangeEvent); isEvent {
		return
	}

	res := patch.Resource()
	if _, seen := b.types[res]; !seen {
		b.order = append(b.order, res)
		b.types[res] = make(map[PatchType]bool)
	}
	b.types[res][patch.PatchType()] = true
}

// deletes returns the resources a delete was buffered for, in first-buffered order.
func (b *bufferedPatches) deletes() []string {
	var deletes []string
	for _, res := range b.order {
		if b.types[res][DeletePatchType] {
			deletes = append(deletes, string(res))
		}
	}

	return deletes
}

// writes returns the resources a create, update, or create-or-update was buffered for (a
// touch is an update), in first-buffered order, and whether any of them buffered a create
// or a create-or-update.
func (b *bufferedPatches) writes() (writes []string, anyCreate bool) {
	for _, res := range b.order {
		types := b.types[res]
		if types[CreatePatchType] || types[CreateOrUpdatePatchType] {
			anyCreate = true
		}
		if types[CreatePatchType] || types[UpdatePatchType] || types[CreateOrUpdatePatchType] {
			writes = append(writes, string(res))
		}
	}

	return writes, anyCreate
}

// referentialMessage composes the client message for a commit Spanner refused with
// FailedPrecondition: a delete of a row other rows still reference, a write whose foreign
// key names a row that does not exist, a required column left empty, or a string longer
// than its column allows. Deletes on one resource, deletes on several, and writes each
// have their own sentence; deletes and writes together, or a record with nothing in it,
// fall back to a sentence that covers both causes, because at commit they cannot be told
// apart.
func (b *bufferedPatches) referentialMessage() string {
	deletes := b.deletes()
	writes, _ := b.writes()

	switch {
	case len(deletes) == 1 && len(writes) == 0:
		return deletes[0] + ": this record cannot be deleted while other records still reference it."
	case len(deletes) > 1 && len(writes) == 0:
		return strings.Join(deletes, ", ") + ": a record cannot be deleted while other records still reference it."
	case len(writes) > 0 && len(deletes) == 0:
		return strings.Join(writes, ", ") + ": a referenced record does not exist, or a value is too long for its field."
	default:
		return referentialRefusalFallback
	}
}

// alreadyExistsMessage composes the client message for a commit Spanner refused with
// AlreadyExists: a create whose key is already taken, or a create or update that
// duplicates a unique-index value. A record with a create cannot tell the two apart and
// names both; a record of updates only can only have duplicated a unique value.
func (b *bufferedPatches) alreadyExistsMessage() string {
	writes, anyCreate := b.writes()

	switch {
	case len(writes) == 0:
		return alreadyExistsFallback
	case anyCreate:
		return strings.Join(writes, ", ") + ": a record with this key or a unique value already exists."
	default:
		return strings.Join(writes, ", ") + ": a unique value already exists on another record."
	}
}

// outOfRangeMessage composes the client message for a commit Spanner refused with
// OutOfRange: a write that violates a CHECK constraint.
func (b *bufferedPatches) outOfRangeMessage() string {
	writes, _ := b.writes()
	if len(writes) == 0 {
		return outOfRangeFallback
	}

	return strings.Join(writes, ", ") + ": a value is outside the range the record allows."
}

// notFoundError composes the error for a commit Spanner refused with NotFound. A record
// of updates only means the row being updated does not exist: 404. A record with a create
// means an interleaved parent does not exist, which is a referenced record: 409, as the
// referential write sentence says. A record with nothing in it gets the sentence covering
// both causes at 409.
func (b *bufferedPatches) notFoundError(err error) error {
	writes, anyCreate := b.writes()

	switch {
	case len(writes) == 0:
		return httpio.NewConflictMessageWithError(err, notFoundFallback)
	case anyCreate:
		return httpio.NewConflictMessageWithError(err, strings.Join(writes, ", ")+": a referenced record does not exist.")
	default:
		return httpio.NewNotFoundMessageWithError(err, strings.Join(writes, ", ")+": this record does not exist.")
	}
}

// translateCommitError turns the error a refused commit returns into the 4xx the caller
// can act on, decided on the gRPC code alone:
//
//   - FailedPrecondition, the code for every referential refusal (a delete of a row other
//     rows still reference through a foreign key without ON DELETE CASCADE or as an
//     interleaved parent under ON DELETE NO ACTION, a create or update whose foreign key
//     names a referenced row that does not exist, a required column left empty) and for a
//     string longer than its column's declared length, answers 409.
//   - AlreadyExists, a duplicate primary key or a duplicate unique-index value, answers 409.
//   - OutOfRange, a violated CHECK constraint, answers 400.
//   - NotFound answers 404 when the record holds updates only (the row being updated does
//     not exist) and 409 when it holds a create (an interleaved parent does not exist). It
//     is also Spanner's code for an unknown column or table, so a deployment whose schema
//     disagrees with the generated code answers 404 or 409 with Spanner's text in the log
//     instead of 500; a schema mismatch fails every write that touches the column, so it
//     does not hide behind one request.
//
// Neither the emulator nor the service attaches structured details, and their message
// texts differ, so the code alone decides and the text is never read. Deletes cannot
// cause any code but FailedPrecondition, so deletes buffered beside writes do not change
// the other codes' sentences. The Spanner error stays the cause in the chain, so the
// server log keeps its full text. Any other code (InvalidArgument, Internal, Aborted, …)
// passes through unchanged.
func translateCommitError(err error, buffered *bufferedPatches) error {
	switch spanner.ErrCode(err) {
	case codes.FailedPrecondition:
		return httpio.NewConflictMessageWithError(err, buffered.referentialMessage())
	case codes.AlreadyExists:
		return httpio.NewConflictMessageWithError(err, buffered.alreadyExistsMessage())
	case codes.OutOfRange:
		return httpio.NewBadRequestMessageWithError(err, buffered.outOfRangeMessage())
	case codes.NotFound:
		return buffered.notFoundError(err)
	default:
		return err
	}
}
