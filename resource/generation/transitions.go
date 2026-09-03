package generation

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/go-playground/errors/v5"
)

// Declared transitions (ABAC design plan §09): an RPC method that moves a
// workflow root along one state edge declares the edge instead of coding it —
// `@transition(Root, from: a, b, to: c)` on the @rpc struct, `@target` on the
// field carrying the target row's key. The generated handler owns the whole
// mechanical frame inside the transaction it already runs: it locates the
// target row within the tenancy predicate (absent or cross-tenant is
// NotFound), verifies the pre-image state is in the from set (outside it is
// Forbidden, naming the transition), runs the body, and stamps the to state
// as the last mutation. The body never reads or writes the state field — it
// carries only the business effect of the edge. Who may move along an edge
// stays the method's Execute grant (§09's grants-only decision).

// The @transition annotation's named arguments.
const (
	transitionFromArgKey = "from"
	transitionToArgKey   = "to"
)

// rpcTransition is a validated @transition declaration, resolved against the
// workflow root it names.
type rpcTransition struct {
	// RootStruct is the root's struct name (the annotation's positional).
	RootStruct string

	// RootResource is the root's registered resource name (pluralized).
	RootResource string

	// From lists the pre-image states the transition may run from, in
	// declaration order.
	From []string

	// To is the state the generated handler stamps after the body returns.
	To string

	// TargetField is the RPC field carrying the target row's key.
	TargetField string

	// RootPKField is the root's key field: the query builder's Set<PKField>
	// setter locates the row, the update patch constructor takes its value.
	RootPKField string

	// StateField is the root's @state field: read for the pre-image check,
	// stamped through Set<StateField> after the body.
	StateField string

	// TenantField is the root's bare tenant-key field, read to locate the row
	// within the tenancy predicate; empty for a global-scoped root.
	TenantField string
}

// FromCases renders the from set as switch-case values: `"draft", "scheduled"`.
func (t *rpcTransition) FromCases() string {
	quoted := make([]string, 0, len(t.From))
	for _, v := range t.From {
		quoted = append(quoted, fmt.Sprintf("%q", v))
	}

	return strings.Join(quoted, ", ")
}

// FromWords renders the from set for error messages: `draft or scheduled`.
func (t *rpcTransition) FromWords() string {
	if len(t.From) == 1 {
		return t.From[0]
	}

	return strings.Join(t.From[:len(t.From)-1], ", ") + " or " + t.From[len(t.From)-1]
}

// resolveTransition compiles and validates an RPC method's @transition and
// @target annotations. Both are optional together: an RPC method without them
// generates exactly what it generates today.
func (c *client) resolveTransition(rpcMethod *rpcMethodInfo, pStruct *parser.Struct, annotations genlang.StructAnnotations) error {
	targetIdx, err := transitionTargetIndex(pStruct, annotations)
	if err != nil {
		return err
	}

	if !annotations.Struct.Has(transitionKeyword) {
		if targetIdx >= 0 {
			return errors.Newf("struct %s field %s: @%s marks the target row key of a @%s declaration, which this struct does not carry", pStruct.Name(), pStruct.Fields()[targetIdx].Name(), targetKeyword, transitionKeyword)
		}

		return nil
	}

	invocations, err := annotations.Struct.Get(transitionKeyword).ParseInvocations(&genlang.ArgSpec{
		Positional: 1,
		Keys:       []string{transitionFromArgKey, transitionToArgKey},
		Required:   []string{transitionFromArgKey, transitionToArgKey},
		Multi:      []string{transitionFromArgKey},
	})
	if err != nil {
		return errors.Wrapf(err, "@%s on %s", transitionKeyword, pStruct.Name())
	}
	rootName := invocations[0].Positional[0]
	from := invocations[0].List(transitionFromArgKey)
	to, _ := invocations[0].Named(transitionToArgKey)

	switch {
	case rpcMethod.SuppressHandler:
		return errors.Newf("struct %s: @%s runs its checks in the generated handler, which @%s suppresses — remove one of them", pStruct.Name(), transitionKeyword, suppressKeyword)
	case !rpcMethod.IsTxnRunner():
		return errors.Newf("struct %s: @%s requires the TxnRunner form — the pre-image check and the state stamp run inside the handler's transaction", pStruct.Name(), transitionKeyword)
	case targetIdx < 0:
		return errors.Newf("struct %s: @%s requires exactly one @%s field carrying the target row's key", pStruct.Name(), transitionKeyword, targetKeyword)
	}

	var root *resourceInfo
	for _, res := range c.resources {
		if res.Name() == rootName {
			root = res

			break
		}
	}
	if root == nil {
		return errors.Newf("struct %s: @%s(%s) names an unknown resource struct", pStruct.Name(), transitionKeyword, rootName)
	}
	state := stateField(root)
	if state == nil {
		return errors.Newf("struct %s: @%s(%s): the root carries no @%s field", pStruct.Name(), transitionKeyword, rootName, stateKeyword)
	}
	if scopeOrGlobal(rpcMethod.PermissionScope) != scopeOrGlobal(root.PermissionScope) {
		return errors.Newf("struct %s: @%s(%s): method and root permission scopes differ — a transition never crosses the tenancy structure", pStruct.Name(), transitionKeyword, rootName)
	}

	if err := c.validateTransitionStates(pStruct, rootName, state, from, to); err != nil {
		return err
	}

	pk, err := transitionRootKey(pStruct, rootName, root)
	if err != nil {
		return err
	}
	targetField := pStruct.Fields()[targetIdx]
	if targetField.Type() != pk.Type() {
		return errors.Newf("struct %s field %s: @%s type %s does not match the root key %s.%s (%s)", pStruct.Name(), targetField.Name(), targetKeyword, targetField.Type(), rootName, pk.Name(), pk.Type())
	}

	transition := &rpcTransition{
		RootStruct:   rootName,
		RootResource: c.pluralize(rootName),
		From:         from,
		To:           to,
		TargetField:  targetField.Name(),
		RootPKField:  pk.Name(),
		StateField:   state.Name(),
	}

	if scopeOrGlobal(root.PermissionScope) == accesstypes.DomainPermissionScope {
		if root.DomainBinding == nil || len(root.DomainBinding.Path) > 0 {
			return errors.Newf("struct %s: @%s(%s): the generated tenancy check reads the root's own tenant key, so the root needs a bare-column @%s binding", pStruct.Name(), transitionKeyword, rootName, domainKeyword)
		}
		transition.TenantField = root.DomainBinding.Anchor.Name()
	}

	rpcMethod.Transition = transition

	return nil
}

// transitionTargetIndex finds the @target field, -1 when none is declared.
func transitionTargetIndex(pStruct *parser.Struct, annotations genlang.StructAnnotations) (int, error) {
	targetIdx := -1
	for i, field := range pStruct.Fields() {
		if !annotations.Fields[i].Has(targetKeyword) {
			continue
		}
		if targetIdx >= 0 {
			return 0, errors.Newf("struct %s: @%s appears on fields %s and %s — a transition addresses exactly one target row", pStruct.Name(), targetKeyword, pStruct.Fields()[targetIdx].Name(), field.Name())
		}
		targetIdx = i
	}

	return targetIdx, nil
}

// validateTransitionStates checks every from/to value against the root's
// state enum table and rejects duplicate from values.
func (c *client) validateTransitionStates(pStruct *parser.Struct, rootName string, state *resourceField, from []string, to string) error {
	values := c.enumValues[state.ReferencedResource]
	isValue := func(v string) bool {
		return slices.ContainsFunc(values, func(e *enumData) bool { return e.ID == v })
	}
	for i, v := range from {
		if !isValue(v) {
			return errors.Newf("struct %s: @%s(%s): %q is not a value of the state enum table %q", pStruct.Name(), transitionKeyword, rootName, v, state.ReferencedResource)
		}
		if slices.Contains(from[:i], v) {
			return errors.Newf("struct %s: @%s(%s): %q appears twice in the from set", pStruct.Name(), transitionKeyword, rootName, v)
		}
	}
	if !isValue(to) {
		return errors.Newf("struct %s: @%s(%s): %q is not a value of the state enum table %q", pStruct.Name(), transitionKeyword, rootName, to, state.ReferencedResource)
	}

	return nil
}

// transitionRootKey resolves the root's single-column primary key — what the
// @target field addresses.
func transitionRootKey(pStruct *parser.Struct, rootName string, root *resourceInfo) (*resourceField, error) {
	if root.PkCount != 1 {
		return nil, errors.Newf("struct %s: @%s(%s): the root's primary key spans %d columns — a @%s field can only address a single-column key", pStruct.Name(), transitionKeyword, rootName, root.PkCount, targetKeyword)
	}
	for _, f := range root.Fields {
		if f.IsPrimaryKey {
			return f, nil
		}
	}

	return nil, errors.Newf("struct %s: @%s(%s): the root has no primary-key field", pStruct.Name(), transitionKeyword, rootName)
}

// rejectTransitionAnnotations rejects @transition and @target outside @rpc
// structs — the declaration describes a generated RPC handler's frame and
// resolves against nothing else.
func rejectTransitionAnnotations(pStruct *parser.Struct, annotations genlang.StructAnnotations, kind string) error {
	var errs []error
	if annotations.Struct.Has(transitionKeyword) {
		errs = append(errs, errors.Newf("struct %s: @%s is only valid on @%s structs; a %s declares no handler to frame", pStruct.Name(), transitionKeyword, rpcKeyword, kind))
	}
	for i, field := range pStruct.Fields() {
		if annotations.Fields[i].Has(targetKeyword) {
			errs = append(errs, errors.Newf("struct %s field %s: @%s is only valid on @%s structs; a %s declares no transition to target", pStruct.Name(), field.Name(), targetKeyword, rpcKeyword, kind))
		}
	}
	if len(errs) > 0 {
		return errors.Wrap(errors.Join(errs...), "transition annotation error")
	}

	return nil
}
