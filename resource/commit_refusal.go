package resource

import (
	"strings"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"google.golang.org/grpc/codes"
)

// referentialRefusalFallback is the sentence for a refusal whose cause the record cannot
// tell apart: deletes and writes buffered together, or nothing buffered at all.
const referentialRefusalFallback = "The request could not be applied: a deleted record is still referenced, or a referenced record does not exist."

// bufferedPatches is the record a write transaction keeps of what it buffered: every
// resource in first-buffered order, each with the patch types buffered for it. When
// Spanner refuses the commit, the record is all the library knows about what the refusal
// concerns, and the client message is composed from it alone: the resource names the
// caller addressed, never the referencing table, the constraint, the key, or a count.
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

// refusalMessage composes the client message for a commit Spanner refused for a
// referential reason, from the record alone. Deletes on one resource, deletes on several,
// and writes (create, update, create-or-update; a touch is an update) each have their own
// sentence; deletes and writes together, or a record with nothing in it, fall back to a
// sentence that covers both causes, because at commit they cannot be told apart.
func (b *bufferedPatches) refusalMessage() string {
	var deletes, writes []string
	for _, res := range b.order {
		types := b.types[res]
		if types[DeletePatchType] {
			deletes = append(deletes, string(res))
		}
		if types[CreatePatchType] || types[UpdatePatchType] || types[CreateOrUpdatePatchType] {
			writes = append(writes, string(res))
		}
	}

	switch {
	case len(deletes) == 1 && len(writes) == 0:
		return deletes[0] + ": this record cannot be deleted while other records still reference it."
	case len(deletes) > 1 && len(writes) == 0:
		return strings.Join(deletes, ", ") + ": a record cannot be deleted while other records still reference it."
	case len(writes) > 0 && len(deletes) == 0:
		return strings.Join(writes, ", ") + ": a referenced record does not exist."
	default:
		return referentialRefusalFallback
	}
}

// translateCommitError turns the error a refused commit returns into the 409 the caller
// can act on. Spanner reports every referential refusal with the gRPC code
// FailedPrecondition: a delete of a row other rows still reference through a foreign key
// without ON DELETE CASCADE or as an interleaved parent under ON DELETE NO ACTION, a
// create or update whose foreign key names a referenced row that does not exist, and a
// required column left empty. Neither the emulator nor the service attaches structured
// details, and their message texts differ, so the code alone decides and the text is
// never read. The Spanner error stays the cause in the chain, so the server log keeps its
// full text. Any other error passes through unchanged.
func translateCommitError(err error, buffered *bufferedPatches) error {
	if spanner.ErrCode(err) != codes.FailedPrecondition {
		return err
	}

	return httpio.NewConflictMessageWithError(err, buffered.refusalMessage())
}
