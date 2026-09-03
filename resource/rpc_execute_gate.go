package resource

import (
	"context"
	"fmt"
	"net/http"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/accesstypes/condition"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
	"google.golang.org/api/iterator"
)

// This file evaluates row-referencing Execute conditions (ABAC design plan
// §12): an RPC method that declares a @target row locates it inside its own
// transaction, and that located row is where a condition on the method's
// Execute grant evaluates — the same pre-image the transition frame's
// from-set check reads. The decoder carries the caller's Conditional decision
// to the generated frame as an ExecuteGate; the frame enforces it after its
// own NotFound/tenancy/state checks and before the body runs. Methods without
// a target row keep the decode-time posture: MigrateRoles rejects
// row-referencing conditions on them, and a Conditional decision reaching
// their decode is a 500-class invariant breach.

// TargetedRPCDecoder decodes an HTTP request for a @target-bearing RPC
// method. Unlike RPCDecoder, a Conditional Execute decision here is not an
// invariant breach: the generated handler has a located row to evaluate it
// against, so Decode carries the condition forward as an ExecuteGate.
type TargetedRPCDecoder[Request any] struct {
	d               *StructDecoder[Request]
	res             accesstypes.Resource
	perm            accesstypes.Permission
	userPermissions func(*http.Request) UserPermissions
	collection      *GeneratedCollection
}

// MustNewTargetedRPCDecoder builds a decoder for a @target-bearing RPC method
// request, wired to the generated collection so a carried condition can lower
// against the target resource's bindings. It panics on construction errors:
// they are programming errors (a malformed request struct), surfaced at
// application startup where generated handlers construct their decoders.
func MustNewTargetedRPCDecoder[Request any](a DecoderAccessor, collection *GeneratedCollection, methodName accesstypes.Resource, perm accesstypes.Permission) *TargetedRPCDecoder[Request] {
	decoder, err := NewStructDecoder[Request]()
	if err != nil {
		panic(errors.Wrap(err, "NewStructDecoder()"))
	}

	return &TargetedRPCDecoder[Request]{
		d:               decoder.WithValidator(a.Validator()),
		res:             methodName,
		perm:            perm,
		userPermissions: a.UserPermissions,
		collection:      collection,
	}
}

// WithValidator sets a validator function on the decoder.
func (s *TargetedRPCDecoder[Request]) WithValidator(v ValidatorFunc) *TargetedRPCDecoder[Request] {
	decoder := *s
	decoder.d = s.d.WithValidator(v)

	return &decoder
}

// Decode decodes the HTTP request body into the Request struct and checks the
// caller's Execute permission in the given scope. Denied is Forbidden here;
// Granted and Conditional both admit — a Conditional decision's payload rides
// the returned ExecuteGate, which the generated frame enforces against the
// located target row inside its transaction.
func (s *TargetedRPCDecoder[Request]) Decode(request *http.Request, scope accesstypes.Scope) (*Request, *ExecuteGate, error) {
	req, err := s.d.Decode(request)
	if err != nil {
		return nil, nil, errors.Wrap(err, "resource.StructDecoder.Decode()")
	}

	userPermissions := s.userPermissions(request)
	env := newRequestEnvironment()
	decisions, err := userPermissions.Check(request.Context(), env, scope, s.perm, s.res)
	if err != nil {
		return nil, nil, errors.Wrap(err, "resource.UserPermissions.Check()")
	}
	if denied := decisions.DeniedResources(); len(denied) > 0 {
		return nil, nil, httpio.NewForbiddenMessagef("user %s, scope %s, does not have %s on %s", userPermissions.User(), scope, s.perm, denied)
	}

	gate := &ExecuteGate{
		method:     s.res,
		user:       userPermissions.User(),
		scope:      scope,
		env:        env,
		collection: s.collection,
	}
	if decision, ok := decisions[s.res]; ok && decision.IsConditional() {
		expr, err := conditionalExpr(s.res, decision)
		if err != nil {
			return nil, nil, err
		}
		gate.cond = expr
	}

	return req, gate, nil
}

// ExecuteTarget describes the located row an ExecuteGate evaluates against.
// The generated frame supplies it from generation-time facts.
type ExecuteTarget struct {
	// Resource is the target row resource (its table).
	Resource accesstypes.Resource

	// Label is the target's struct name, for the refusal message — the same
	// words the frame's own state check refuses with, so the wire never
	// distinguishes a wrong-state refusal from a condition refusal.
	Label string

	// PKColumn is the primary-key column the check-SELECT locates the row by.
	PKColumn string
}

// targetKeyParamName locates the target row in the gate's check-SELECT; the
// zz prefix keeps it outside the lowering registry's @_c namespace and the
// reserved subject/now/domain vocabulary.
const targetKeyParamName = "zzTargetKey"

// ExecuteGate carries one request's Execute decision to the generated frame.
// A nil condition (the grant was unconditional) enforces nothing.
type ExecuteGate struct {
	method     accesstypes.Resource
	user       accesstypes.User
	scope      accesstypes.Scope
	env        accesstypes.Environment
	collection *GeneratedCollection
	cond       condition.Expr
}

// Enforce evaluates the carried condition against the target row inside the
// frame's transaction: one check-SELECT locating the row by key, its single
// boolean the condition lowered against the target resource's bindings, bound
// to the same @subject, @now, and @domain the permission check folded with. A
// false or NULL answer is Forbidden with the frame's uniform refusal — the
// wire never says whether the state or the condition refused.
func (g *ExecuteGate) Enforce(ctx context.Context, txn ReadWriteTransaction, target ExecuteTarget, pkValue any) error {
	if g == nil || g.cond == nil {
		return nil
	}
	if txn.DBType() != SpannerDBType {
		return errors.Newf("execute-condition enforcement is not implemented for %s", txn.DBType())
	}
	if g.collection == nil {
		return errors.Newf("method %s carries a conditional Execute decision but no generated collection is wired to render it", g.method)
	}

	stmt, err := g.checkStatement(target, pkValue)
	if err != nil {
		return err
	}

	it := txn.SpannerReadOnlyTransaction().Query(ctx, stmt.SpannerStatement())
	defer it.Stop()

	row, err := it.Next()
	if err != nil {
		if errors.Is(err, iterator.Done) {
			return httpio.NewNotFoundMessagef("%s %v does not exist", target.Label, pkValue)
		}

		return errors.Wrap(err, "spanner.RowIterator.Next()")
	}

	var passed spanner.NullBool
	if err := row.Column(0, &passed); err != nil {
		return errors.Wrap(err, "spanner.Row.Column()")
	}
	if !passed.Valid || !passed.Bool {
		return httpio.NewForbiddenMessagef("%s may not run against %s %v", g.method, target.Label, pkValue)
	}

	return nil
}

// checkStatement renders the gate's check-SELECT: the condition lowered
// against the target resource's bindings as one boolean, the row located by
// its primary key.
func (g *ExecuteGate) checkStatement(target ExecuteTarget, pkValue any) (*Statement, error) {
	bindings, _ := g.collection.Bindings(g.collection.Scope(target.Resource), target.Resource)
	_, partitioned := g.scope.Domain()
	lctx := &loweringContext{
		outer:       string(target.Resource),
		bindings:    bindings,
		collection:  g.collection,
		partitioned: partitioned,
	}

	gen, err := loweredSQLGenerator(SpannerDBType)
	if err != nil {
		return nil, err
	}
	registry := newParamRegistry()

	sql, err := lowerToSQL(g.cond, lctx, gen, registry)
	if err != nil {
		return nil, errors.Wrapf(err, "method %s: lowering the Execute condition against %s", g.method, target.Resource)
	}

	params := map[string]any{targetKeyParamName: pkValue}
	for _, param := range registry.boundParams() {
		if _, ok := params[param.Name]; ok {
			return nil, errors.Newf("named parameter collision: %s check statement already contains named parameter %q", g.method, param.Name)
		}
		params[param.Name] = param.Value
	}
	for _, name := range registry.referencedNames() {
		value, err := g.reservedParamValue(name)
		if err != nil {
			return nil, err
		}
		params[name] = value
	}

	return &Statement{
		SQL:    fmt.Sprintf("SELECT (%s) AS g0 FROM %s WHERE %s = @%s", sql, target.Resource, target.PKColumn, targetKeyParamName),
		Params: params,
	}, nil
}

// reservedParamValue supplies a referenced reserved parameter's value: the
// same values the permission check folded with, never re-sampled.
func (g *ExecuteGate) reservedParamValue(name string) (any, error) {
	switch name {
	case subjectParamName:
		return string(g.user), nil
	case nowParamName:
		now, ok := g.env.Now()
		if !ok {
			return nil, errors.New("condition references now but the request environment carries no decision instant")
		}

		return now, nil
	case domainParamName:
		domain, ok := g.scope.Domain()
		if !ok {
			return nil, errors.New("condition rendering referenced the request partition in a global request")
		}

		return string(domain), nil
	default:
		return nil, errors.Newf("condition rendering referenced unknown named parameter %q", name)
	}
}
