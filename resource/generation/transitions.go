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

// Declared transitions and located-row targets (ABAC design plan §09, §12):
// an RPC method that moves a workflow root along one state edge declares the
// edge instead of coding it — `@transition(Root, from: a, b, to: c)` on the
// @rpc struct, `@target` on the field carrying the target row's key. The
// generated handler owns the whole mechanical frame inside the transaction it
// already runs: it locates the target row within the tenancy predicate
// (absent or cross-tenant is NotFound), verifies the pre-image state is in
// the from set, evaluates any row-referencing Execute condition the caller's
// grant carries against the same row, runs the body, and stamps the to state
// as the last mutation. The body never reads or writes the state field — it
// carries only the business effect of the edge.
//
// A method that moves no state can still declare a target row —
// `@target(Root)` names the resource directly — and gets the plain form of
// the frame: locate, tenancy, condition, body, no state check and no stamp.
// Either way, who may run the method stays its Execute grant; a target is
// what lets that grant carry a row-referencing condition (§12).

// The @transition annotation's named arguments.
const (
	transitionFromArgKey = "from"
	transitionToArgKey   = "to"
)

// rpcTarget is a validated @target declaration: the row the generated
// handler locates inside its transaction before the body runs. A
// transition's target root is named by its @transition; a plain method names
// it directly with @target(Root).
type rpcTarget struct {
	// RootStruct is the target's struct name.
	RootStruct string

	// RootResource is the target's registered resource name (pluralized).
	RootResource string

	// TargetField is the RPC field carrying the target row's key.
	TargetField string

	// RootPKField is the target's key field: the query builder's Set<PKField>
	// setter locates the row, the update patch constructor takes its value.
	RootPKField string

	// RootPKColumn is the key field's database column — the Execute-condition
	// check-SELECT locates the row by it.
	RootPKColumn string

	// StateField is the root's @state field, read for a transition's
	// pre-image check and stamped through Set<StateField> after the body;
	// empty for the plain form, which never touches state.
	StateField string

	// TenantField is the target's bare tenant-key field, read to locate the
	// row within the tenancy predicate; empty for a global-scoped target.
	TenantField string
}

// LocateColumnCalls renders the located-row read's column accessors: the
// state and tenant fields when the frame reads them, else the key itself —
// the read exists to prove the row, so it always selects something.
func (t *rpcTarget) LocateColumnCalls() string {
	var b strings.Builder
	if t.StateField != "" {
		b.WriteString("." + t.StateField + "()")
	}
	if t.TenantField != "" {
		b.WriteString("." + t.TenantField + "()")
	}
	if b.Len() == 0 {
		b.WriteString("." + t.RootPKField + "()")
	}

	return b.String()
}

// rpcTransition is a validated @transition declaration, resolved against the
// workflow root it names.
type rpcTransition struct {
	rpcTarget

	// From lists the pre-image states the transition may run from, in
	// declaration order.
	From []string

	// To is the state the generated handler stamps after the body returns.
	To string
}

// FromCases renders the from set as switch-case values: `"draft", "scheduled"`.
func (t *rpcTransition) FromCases() string {
	quoted := make([]string, 0, len(t.From))
	for _, v := range t.From {
		quoted = append(quoted, fmt.Sprintf("%q", v))
	}

	return strings.Join(quoted, ", ")
}

// resolveTransition compiles and validates an RPC method's @transition and
// @target annotations. An RPC method without either generates exactly what it
// generates today; @target alone is the plain located-row form.
func (c *client) resolveTransition(rpcMethod *rpcMethodInfo, pStruct *parser.Struct, annotations genlang.StructAnnotations) error {
	targetIdx, err := transitionTargetIndex(pStruct, annotations)
	if err != nil {
		return err
	}

	if !annotations.Struct.Has(transitionKeyword) {
		if targetIdx < 0 {
			return nil
		}

		return c.resolvePlainTarget(rpcMethod, pStruct, annotations, targetIdx)
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

	if targetIdx < 0 {
		return errors.Newf("struct %s: @%s requires exactly one @%s field carrying the target row's key", pStruct.Name(), transitionKeyword, targetKeyword)
	}
	if arg := annotations.Fields[targetIdx].Get(targetKeyword); len(arg) > 0 {
		return errors.Newf("struct %s field %s: with @%s, @%s takes no argument — the declared root is the target", pStruct.Name(), pStruct.Fields()[targetIdx].Name(), transitionKeyword, targetKeyword)
	}

	target, root, err := c.resolveTarget(rpcMethod, pStruct, targetIdx, rootName, transitionKeyword)
	if err != nil {
		return err
	}

	state := stateField(root)
	if state == nil {
		return errors.Newf("struct %s: @%s(%s): the root carries no @%s field", pStruct.Name(), transitionKeyword, rootName, stateKeyword)
	}
	target.StateField = state.Name()

	if err := c.validateTransitionStates(pStruct, rootName, state, from, to); err != nil {
		return err
	}

	rpcMethod.Transition = &rpcTransition{rpcTarget: *target, From: from, To: to}
	rpcMethod.Target = &rpcMethod.Transition.rpcTarget

	return nil
}

// resolvePlainTarget compiles @target(Root) without a @transition: the plain
// located-row form (design plan §12). The generated frame locates the row
// (absent or cross-tenant is NotFound) and evaluates any row-referencing
// Execute condition against it; there is no pre-image state check and no
// stamp — the method moves no state.
func (c *client) resolvePlainTarget(rpcMethod *rpcMethodInfo, pStruct *parser.Struct, annotations genlang.StructAnnotations, targetIdx int) error {
	field := pStruct.Fields()[targetIdx]
	invocations, err := annotations.Fields[targetIdx].Get(targetKeyword).ParseInvocations(&genlang.ArgSpec{Positional: 1})
	if err != nil || len(invocations) == 0 {
		if err == nil {
			err = errors.New("missing argument")
		}

		return errors.Wrapf(err, "struct %s field %s: without @%s, @%s(Root) names the row resource the handler locates", pStruct.Name(), field.Name(), transitionKeyword, targetKeyword)
	}
	rootName := invocations[0].Positional[0]

	target, _, err := c.resolveTarget(rpcMethod, pStruct, targetIdx, rootName, targetKeyword)
	if err != nil {
		return err
	}

	rpcMethod.Target = target

	return nil
}

// resolveTarget validates what both target forms share: the handler frame's
// preconditions and the named root's shape, resolved into an rpcTarget.
func (c *client) resolveTarget(rpcMethod *rpcMethodInfo, pStruct *parser.Struct, targetIdx int, rootName, declKeyword string) (*rpcTarget, *resourceInfo, error) {
	switch {
	case rpcMethod.SuppressHandler:
		return nil, nil, errors.Newf("struct %s: @%s runs its checks in the generated handler, which @%s suppresses — remove one of them", pStruct.Name(), declKeyword, suppressKeyword)
	case !rpcMethod.IsTxnRunner():
		return nil, nil, errors.Newf("struct %s: @%s requires the TxnRunner form — the located-row checks run inside the handler's transaction", pStruct.Name(), declKeyword)
	}

	var root *resourceInfo
	for _, res := range c.resources {
		if res.Name() == rootName {
			root = res

			break
		}
	}
	if root == nil {
		return nil, nil, errors.Newf("struct %s: @%s(%s) names an unknown resource struct", pStruct.Name(), declKeyword, rootName)
	}
	if scopeOrGlobal(rpcMethod.PermissionScope) != scopeOrGlobal(root.PermissionScope) {
		return nil, nil, errors.Newf("struct %s: @%s(%s): method and target permission scopes differ — a located-row check never crosses the tenancy structure", pStruct.Name(), declKeyword, rootName)
	}

	pk, err := targetRootKey(pStruct, rootName, root, declKeyword)
	if err != nil {
		return nil, nil, err
	}
	targetField := pStruct.Fields()[targetIdx]
	if targetField.Type() != pk.Type() {
		return nil, nil, errors.Newf("struct %s field %s: @%s type %s does not match the root key %s.%s (%s)", pStruct.Name(), targetField.Name(), targetKeyword, targetField.Type(), rootName, pk.Name(), pk.Type())
	}

	target := &rpcTarget{
		RootStruct:   rootName,
		RootResource: c.pluralize(rootName),
		TargetField:  targetField.Name(),
		RootPKField:  pk.Name(),
		RootPKColumn: fieldColumn(pk),
	}

	if scopeOrGlobal(root.PermissionScope) == accesstypes.DomainPermissionScope {
		if root.DomainBinding == nil || len(root.DomainBinding.Path) > 0 {
			return nil, nil, errors.Newf("struct %s: @%s(%s): the generated tenancy check reads the root's own tenant key, so the root needs a bare-column @%s binding", pStruct.Name(), declKeyword, rootName, domainKeyword)
		}
		target.TenantField = root.DomainBinding.Anchor.Name()
	}

	return target, root, nil
}

// transitionTargetIndex finds the @target field, -1 when none is declared.
func transitionTargetIndex(pStruct *parser.Struct, annotations genlang.StructAnnotations) (int, error) {
	targetIdx := -1
	for i, field := range pStruct.Fields() {
		if !annotations.Fields[i].Has(targetKeyword) {
			continue
		}
		if targetIdx >= 0 {
			return 0, errors.Newf("struct %s: @%s appears on fields %s and %s — a method addresses exactly one target row", pStruct.Name(), targetKeyword, pStruct.Fields()[targetIdx].Name(), field.Name())
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

// targetRootKey resolves the root's single-column primary key — what the
// @target field addresses.
func targetRootKey(pStruct *parser.Struct, rootName string, root *resourceInfo, declKeyword string) (*resourceField, error) {
	if root.PkCount != 1 {
		return nil, errors.Newf("struct %s: @%s(%s): the root's primary key spans %d columns — a @%s field can only address a single-column key", pStruct.Name(), declKeyword, rootName, root.PkCount, targetKeyword)
	}
	for _, f := range root.Fields {
		if f.IsPrimaryKey {
			return f, nil
		}
	}

	return nil, errors.Newf("struct %s: @%s(%s): the root has no primary-key field", pStruct.Name(), declKeyword, rootName)
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
			errs = append(errs, errors.Newf("struct %s field %s: @%s is only valid on @%s structs; a %s declares no handler to frame", pStruct.Name(), field.Name(), targetKeyword, rpcKeyword, kind))
		}
	}
	if len(errs) > 0 {
		return errors.Wrap(errors.Join(errs...), "transition annotation error")
	}

	return nil
}
