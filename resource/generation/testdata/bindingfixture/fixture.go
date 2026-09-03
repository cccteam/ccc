// Package bindingfixture provides parsed-struct fixtures for the binding-annotation
// resolution tests: column and join-path @attribute bindings, the @domain tenancy
// binding, and the subject-side @subjectSet / @subjectValue vocabulary, plus one
// struct per rejection the resolver enforces.
package bindingfixture

import (
	"github.com/cccteam/ccc"
)

// Struct-level annotations only parse from a TypeSpec doc comment, so scoped
// structs sit in grouped type blocks like real project sources do.
type (
	// MaintenanceTask carries the well-formed binding shapes. It is
	// domain-scoped because @domain pairs with domain scope (both directions
	// of the tenancy pairing are rejection shapes below).
	//
	// @permissionScope(domain)
	MaintenanceTask struct {
		ID ccc.UUID `spanner:"Id"`

		// @domain
		StationID ccc.UUID `spanner:"StationId"`

		// @attribute(crew)
		CrewID ccc.UUID `spanner:"CrewId"`

		// @attribute(shipClass, via: Class)
		ShipID ccc.UUID `spanner:"ShipId"`

		// @attribute(sector, via: StationID.Sector)
		BerthID ccc.UUID `spanner:"BerthId"`

		// @attribute(estimatedCost)
		EstimatedCost int64 `spanner:"EstimatedCost"`
	}
)

// Ship is the one-hop target: ShipID resolves shipClass to Ships.Class.
type Ship struct {
	ID    ccc.UUID `spanner:"Id"`
	Class string   `spanner:"Class"`
}

// Berth is the first hop of the two-hop path; Station the second.
type Berth struct {
	ID        ccc.UUID `spanner:"Id"`
	StationID ccc.UUID `spanner:"StationId"`
}

type Station struct {
	ID     ccc.UUID `spanner:"Id"`
	Sector string   `spanner:"Sector"`
}

type (
	// CrewMember anchors the subject-side set vocabulary.
	//
	// @permissionScope(domain)
	CrewMember struct {
		ID ccc.UUID `spanner:"Id"`

		// @subjectSet(crews, value: CrewID)
		UserID ccc.UUID `spanner:"UserId"`

		CrewID ccc.UUID `spanner:"CrewId"`

		// @domain
		StationID ccc.UUID `spanner:"StationId"`
	}
)

// UserProfile anchors the scalar vocabulary; UserID is the primary key.
type UserProfile struct {
	// @subjectValue(approvalLimit, value: ApprovalLimit)
	UserID ccc.UUID `spanner:"UserId"`

	ApprovalLimit int64 `spanner:"ApprovalLimit"`
}

// The rejection shapes, one struct each.

type ReservedName struct {
	ID ccc.UUID `spanner:"Id"`

	// @attribute(new)
	CrewID ccc.UUID `spanner:"CrewId"`
}

type BadCharset struct {
	ID ccc.UUID `spanner:"Id"`

	// @attribute(crew-chief)
	CrewID ccc.UUID `spanner:"CrewId"`
}

type DuplicateName struct {
	ID ccc.UUID `spanner:"Id"`

	// @attribute(crew)
	CrewID ccc.UUID `spanner:"CrewId"`

	// @subjectSet(crew, value: CrewID)
	UserID ccc.UUID `spanner:"UserId"`
}

type PathOffNonFK struct {
	ID ccc.UUID `spanner:"Id"`

	// @attribute(sector, via: Sector)
	Label string `spanner:"Label"`
}

type PathUnknownField struct {
	ID ccc.UUID `spanner:"Id"`

	// @attribute(shipClass, via: Nope)
	ShipID ccc.UUID `spanner:"ShipId"`
}

type PathThroughNonFK struct {
	ID ccc.UUID `spanner:"Id"`

	// @attribute(deep, via: Class.Deeper)
	ShipID ccc.UUID `spanner:"ShipId"`
}

type ScalarOnNonUnique struct {
	ID ccc.UUID `spanner:"Id"`

	// @subjectValue(approvalLimit, value: ApprovalLimit)
	UserID ccc.UUID `spanner:"UserId"`

	ApprovalLimit int64 `spanner:"ApprovalLimit"`
}

type UnknownValueField struct {
	ID ccc.UUID `spanner:"Id"`

	// @subjectSet(crews, value: Nope)
	UserID ccc.UUID `spanner:"UserId"`
}

// Struct-level annotations only parse from a TypeSpec doc comment, so this
// rejection shape sits in a grouped type block like real project sources do.
type (
	// PartitionBlindAnchor is the missing-tenancy rejection: a domain-scoped
	// resource anchoring subject vocabulary with no @domain binding, so
	// subject.<name> would match the user's rows from every tenant.
	//
	// @permissionScope(domain)
	PartitionBlindAnchor struct {
		ID ccc.UUID `spanner:"Id"`

		// @subjectSet(crews, value: CrewID)
		UserID ccc.UUID `spanner:"UserId"`

		CrewID ccc.UUID `spanner:"CrewId"`
	}
)

type DoubleTenant struct {
	ID ccc.UUID `spanner:"Id"`

	// @domain
	StationID ccc.UUID `spanner:"StationId"`

	// @domain
	RegionID ccc.UUID `spanner:"RegionId"`
}

// GlobalTenant is the reverse tenancy-pairing rejection: a @domain binding on
// a resource that is not domain-scoped has no tenant partition to resolve to.
type GlobalTenant struct {
	ID ccc.UUID `spanner:"Id"`

	// @domain
	StationID ccc.UUID `spanner:"StationId"`
}

// TenantStatedTwice restates the bare tenant key's derived decode behavior
// through a conditions tag.
type (
	// @permissionScope(domain)
	TenantStatedTwice struct {
		ID ccc.UUID `spanner:"Id"`

		// @domain
		StationID ccc.UUID `spanner:"StationId" conditions:"immutable"`
	}
)

type (
	// UnboundTenant is the mandatory-binding rejection: domain-scoped with no
	// @domain declared anywhere.
	//
	// @permissionScope(domain)
	UnboundTenant struct {
		ID   ccc.UUID `spanner:"Id"`
		Name string   `spanner:"Name"`
	}
)

// VirtualWithBinding stands in for every schemaless struct kind in the
// rejection test.
type VirtualWithBinding struct {
	ID ccc.UUID `spanner:"Id"`

	// @attribute(crew)
	CrewID ccc.UUID `spanner:"CrewId"`
}

// StatefulTask carries the @state marker; TaskState is the state column's
// enum-backed type.
type TaskState string

type StatefulTask struct {
	ID ccc.UUID `spanner:"Id"`

	// @state(default: open)
	State TaskState `spanner:"State"`

	Notes string `spanner:"Notes"`
}

type StateOnNonFK struct {
	ID ccc.UUID `spanner:"Id"`

	// @state(default: open)
	Label string `spanner:"Label"`
}

type StateBadDefault struct {
	ID ccc.UUID `spanner:"Id"`

	// @state(default: bogus)
	State TaskState `spanner:"State"`
}

type StateStatedTwice struct {
	ID ccc.UUID `spanner:"Id"`

	// @state(default: open)
	State TaskState `spanner:"State" conditions:"output_only"`
}

// TaskPart is a direct workflow member; PartOrder chains through it.
type TaskPart struct {
	ID ccc.UUID `spanner:"Id"`

	// @stateRoot(StatefulTask)
	TaskID ccc.UUID `spanner:"TaskId"`
}

type PartOrder struct {
	ID ccc.UUID `spanner:"Id"`

	// @stateRoot(StatefulTask)
	PartID ccc.UUID `spanner:"PartId"`
}

type UnknownRootMember struct {
	ID ccc.UUID `spanner:"Id"`

	// @stateRoot(Mystery)
	TaskID ccc.UUID `spanner:"TaskId"`
}

type ChainBreakMember struct {
	ID ccc.UUID `spanner:"Id"`

	// @stateRoot(StatefulTask) — the FK lands outside the workflow
	ShipID ccc.UUID `spanner:"ShipId"`
}

type (
	// @permissionScope(domain)
	ScopedMember struct {
		ID ccc.UUID `spanner:"Id"`

		// @stateRoot(StatefulTask)
		TaskID ccc.UUID `spanner:"TaskId"`
	}
)

// TxnRunner mirrors the generated RPC interface's name so transition fixtures
// can implement it — the extractor matches interfaces by name.
type TxnRunner interface {
	RunsInTxn()
}

// The transition fixtures: one well-formed shape and one struct per rejection
// the resolver enforces.
type (
	// ApproveTask is the well-formed declared transition.
	//
	// @rpc
	// @transition(StatefulTask, from: open, to: approved)
	ApproveTask struct {
		// @target
		TaskID ccc.UUID
		Note   string
	}

	// CloseTask exercises the multi-valued from set.
	//
	// @rpc
	// @transition(StatefulTask, from: open, approved, to: closed)
	CloseTask struct {
		// @target
		TaskID ccc.UUID
	}

	// @rpc
	// @transition(Mystery, from: open, to: approved)
	TransitionUnknownRoot struct {
		// @target
		TaskID ccc.UUID
	}

	// @rpc
	// @transition(Ship, from: open, to: approved)
	TransitionStatelessRoot struct {
		// @target
		TaskID ccc.UUID
	}

	// @rpc
	// @transition(StatefulTask, from: bogus, to: approved)
	TransitionBadFrom struct {
		// @target
		TaskID ccc.UUID
	}

	// @rpc
	// @transition(StatefulTask, from: open, open, to: approved)
	TransitionDuplicateFrom struct {
		// @target
		TaskID ccc.UUID
	}

	// @rpc
	// @transition(StatefulTask, from: open, to: bogus)
	TransitionBadTo struct {
		// @target
		TaskID ccc.UUID
	}

	// @rpc
	// @transition(StatefulTask, from: open, to: approved)
	TransitionNoTarget struct {
		TaskID ccc.UUID
	}

	// @rpc
	// @transition(StatefulTask, from: open, to: approved)
	TransitionTwoTargets struct {
		// @target
		TaskID ccc.UUID
		// @target
		OtherID ccc.UUID
	}

	// @rpc
	// @transition(StatefulTask, from: open, to: approved)
	TransitionTargetTypeMismatch struct {
		// @target
		TaskID string
	}

	// @rpc
	// @transition(StatefulTask, from: open, to: approved)
	TransitionNotTxnRunner struct {
		// @target
		TaskID ccc.UUID
	}

	// @rpc
	// @permissionScope(domain)
	// @transition(StatefulTask, from: open, to: approved)
	TransitionScopeMismatch struct {
		// @target
		TaskID ccc.UUID
	}

	// @rpc
	TargetWithoutTransition struct {
		// @target
		TaskID ccc.UUID
	}
)

func (ApproveTask) RunsInTxn()                  {}
func (CloseTask) RunsInTxn()                    {}
func (TransitionUnknownRoot) RunsInTxn()        {}
func (TransitionStatelessRoot) RunsInTxn()      {}
func (TransitionBadFrom) RunsInTxn()            {}
func (TransitionDuplicateFrom) RunsInTxn()      {}
func (TransitionBadTo) RunsInTxn()              {}
func (TransitionNoTarget) RunsInTxn()           {}
func (TransitionTwoTargets) RunsInTxn()         {}
func (TransitionTargetTypeMismatch) RunsInTxn() {}
func (TransitionScopeMismatch) RunsInTxn()      {}
func (TargetWithoutTransition) RunsInTxn()      {}
