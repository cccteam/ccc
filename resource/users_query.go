package resource

import (
	"context"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/accesstypes/condition"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

// The user surface's query path. A composed resource has no query for a condition to be
// lowered into and no row before the request composes one, so the decoder here keeps
// the Conditional decisions the computed path refuses, and the handler judges each
// composed row against them: a row the resource-level condition refuses is left out, a
// cell whose field-level condition refuses is masked, and the capability envelope
// answers from the same judgement.

// composedQuery is one decoded list or read: the query set and the decisions the
// composition judges rows against.
type composedQuery[Row Resourcer] struct {
	qSet *QuerySet[Row]
	// resourceDecision is the decision on the resource; fieldDecisions the Conditional
	// decisions on the projected fields, by field.
	resourceDecision accesstypes.Decision
	fieldDecisions   map[accesstypes.Field]accesstypes.Decision
}

// decodeComposed decodes a request for a composed resource and checks it: a denied
// resource or an explicitly requested denied field is Forbidden, a sort or filter field
// needs an unconditional grant, the default projection is every field some grant
// mentions, and the Conditional decisions are kept for the rows.
func decodeComposed[Row Resourcer, Request any](d *QueryDecoder[Row, Request], r *http.Request, userPermissions UserPermissions, scope accesstypes.Scope, dbType DBType) (*composedQuery[Row], error) {
	qSet, err := d.DecodeWithoutPermissions(r)
	if err != nil {
		return nil, err
	}
	rSet := d.resourceSet
	perms := rSet.Permissions()
	if len(perms) != 1 {
		return nil, errors.Newf("resource: a composed decoder takes one permission, found %d (%s)", len(perms), perms)
	}
	required := perms[0]
	qSet.scope = scope
	qSet.requiredPermission = required
	qSet.userPermissions = userPermissions
	qSet.dbType = dbType
	if err := qSet.bindCursor(scope); err != nil {
		return nil, err
	}

	ctx := r.Context()
	decisions, err := userPermissions.Check(ctx, qSet.env, scope, required, rSet.BaseResource())
	if err != nil {
		return nil, errors.Wrap(err, "resource.UserPermissions.Check()")
	}
	if denied := decisions.DeniedResources(); len(denied) > 0 {
		return nil, httpio.NewForbiddenMessagef("scope (%s), user (%s) does not have (%s) on %s", scope, userPermissions.User(), required, denied)
	}
	cq := &composedQuery[Row]{qSet: qSet, resourceDecision: decisions[rSet.BaseResource()], fieldDecisions: make(map[accesstypes.Field]accesstypes.Decision)}

	if err := qSet.checkQueryFieldsGranted(ctx, rSet, userPermissions); err != nil {
		return nil, err
	}

	explicit := len(qSet.Fields()) > 0
	candidates := qSet.Fields()
	if !explicit {
		candidates = d.visibleFields()
	}
	var guarded []accesstypes.Field
	var resources []accesstypes.Resource
	for _, field := range candidates {
		if rSet.PermissionRequired(field, required) {
			guarded = append(guarded, field)
			resources = append(resources, rSet.Resource(field))
		}
	}
	if len(resources) > 0 {
		decisions, err = userPermissions.Check(ctx, qSet.env, scope, required, resources...)
		if err != nil {
			return nil, errors.Wrap(err, "resource.UserPermissions.Check()")
		}
	}
	if explicit {
		if denied := decisions.DeniedResources(); len(denied) > 0 {
			return nil, httpio.NewForbiddenMessagef("scope (%s), user (%s) does not have (%s) on %s", scope, userPermissions.User(), required, denied)
		}
	} else {
		qSet.fields = nil
		for _, field := range candidates {
			if slices.Contains(guarded, field) && decisions[rSet.Resource(field)].IsDenied() {
				continue
			}
			qSet.AddField(field)
		}
	}
	for _, field := range guarded {
		if decision := decisions[rSet.Resource(field)]; decision.IsConditional() && slices.Contains(qSet.Fields(), field) {
			cq.fieldDecisions[field] = decision
		}
	}

	return cq, nil
}

// userJudge evaluates decisions over composed rows for one request: the caller, the
// decision instant, and the caller's own facts for the subject bindings, resolved once
// the first condition asks for them.
type userJudge struct {
	snap     *userSnapshot
	user     accesstypes.User
	now      time.Time
	resolved bool
	subjects struct {
		values map[string]any
		sets   map[string][]any
	}
}

func newUserJudge(snap *userSnapshot, userPermissions UserPermissions) *userJudge {
	return &userJudge{snap: snap, user: userPermissions.User(), now: time.Now()}
}

// permits judges one decision against a row image: Granted permits, Denied refuses, and
// a Conditional decision permits when every group's condition is true over the image.
func (j *userJudge) permits(ctx context.Context, decision accesstypes.Decision, pre, post map[string]any) (bool, error) {
	switch {
	case decision.IsGranted():
		return true, nil
	case decision.IsDenied():
		return false, nil
	}
	for _, variant := range roleVariants(pre, post) {
		for _, group := range decision.ConditionGroups() {
			expr := group.Condition.Expr()
			if expr == nil {
				continue
			}
			image, err := j.image(ctx, expr, variant[0], variant[1])
			if err != nil {
				return false, err
			}
			truth, err := condition.Evaluate(expr, image)
			if err != nil {
				return false, errors.Wrap(err, "condition.Evaluate()")
			}
			if !truth.Permits() {
				return false, nil
			}
		}
	}

	return true, nil
}

// roleVariants expands an image whose role attribute is a list of roles, an
// invitation's, into one image per role, so a condition on role holds for the row only
// when it holds for every role it carries; an image with no list is itself. Each
// variant is the pre- and post-image pair.
func roleVariants(pre, post map[string]any) [][2]map[string]any {
	roles, listed := roleList(pre)
	inPost := false
	if !listed {
		if roles, listed = roleList(post); !listed {
			return [][2]map[string]any{{pre, post}}
		}
		inPost = true
	}
	if len(roles) == 0 {
		return [][2]map[string]any{{pre, post}}
	}
	source := pre
	if inPost {
		source = post
	}
	variants := make([][2]map[string]any, 0, len(roles))
	for _, role := range roles {
		image := make(map[string]any, len(source))
		maps.Copy(image, source)
		image[seatAttrRole] = role
		if inPost {
			variants = append(variants, [2]map[string]any{pre, image})
		} else {
			variants = append(variants, [2]map[string]any{image, post})
		}
	}

	return variants
}

// roleList reads the role attribute of an image as a list, when it is one.
func roleList(image map[string]any) ([]string, bool) {
	if image == nil {
		return nil, false
	}
	roles, ok := image[seatAttrRole].([]string)

	return roles, ok
}

// image assembles the evaluator's image for one condition, resolving the caller's own
// facts when the condition names a subject binding.
func (j *userJudge) image(ctx context.Context, expr condition.Expr, pre, post map[string]any) (*condition.Image, error) {
	image := &condition.Image{Pre: pre, Post: post, Subject: string(j.user), Now: j.now, Zone: localZone.Load()}
	if len(condition.SubjectSets(expr)) > 0 || len(condition.SubjectValues(expr)) > 0 {
		if err := j.resolveSubjects(ctx); err != nil {
			return nil, err
		}
		image.SubjectValues = j.subjects.values
		image.SubjectSets = j.subjects.sets
	}

	return image, nil
}

// resolveSubjects reads the caller's own row in the Auth: their organization and the
// roles they hold in the partition. A caller with no row here (one managing another
// Auth's users) has no facts, and a condition that asks for them refuses.
func (j *userJudge) resolveSubjects(ctx context.Context) error {
	if j.resolved {
		return nil
	}
	j.resolved = true
	j.subjects.values = map[string]any{}
	j.subjects.sets = map[string][]any{}
	account := j.snap.byUsername(string(j.user))
	if account == nil {
		return nil
	}
	facts, err := j.snap.factsOf(ctx, account)
	if err != nil {
		return err
	}
	if facts.organization != nil {
		j.subjects.values[UserSubjectOrganization] = *facts.organization
	}
	var roles []any
	for _, scope := range j.snap.partition.seatScopes {
		for _, role := range j.snap.heldRoles(account.Username, scope) {
			roles = append(roles, role)
		}
	}
	j.subjects.sets[UserSubjectRoles] = roles

	return nil
}

// self reports whether the row's user is the caller.
func (j *userJudge) self(username string) bool {
	return sameUser(string(j.user), username)
}

// checkWriteGrants is the first half of a write's check: Denied is Forbidden naming the
// resources, before any store is read, so a caller without the grant learns nothing of
// the rows. The decisions come back for judgeWrite once the row is composed.
func checkWriteGrants(ctx context.Context, userPermissions UserPermissions, scope accesstypes.Scope, perm accesstypes.Permission, res accesstypes.Resource, fields []accesstypes.Resource) (accesstypes.Decisions, error) {
	resources := make([]accesstypes.Resource, 0, 1+len(fields))
	resources = append(resources, res)
	resources = append(resources, fields...)
	decisions, err := userPermissions.Check(ctx, RequestEnvironment(), scope, perm, resources...)
	if err != nil {
		return nil, errors.Wrap(err, "resource.UserPermissions.Check()")
	}
	if denied := decisions.DeniedResources(); len(denied) > 0 {
		return nil, httpio.NewForbiddenMessagef("scope (%s), user (%s) does not have (%s) on %s", scope, userPermissions.User(), perm, denied)
	}

	return decisions, nil
}

// judgeWrite is the second half: every Conditional decision on the resource or a field
// is judged over the write's pre- and post-image.
func (j *userJudge) judgeWrite(ctx context.Context, decisions accesstypes.Decisions, scope accesstypes.Scope, perm accesstypes.Permission, pre, post map[string]any) error {
	for checked, decision := range decisions {
		ok, err := j.permits(ctx, decision, pre, post)
		if err != nil {
			return err
		}
		if !ok {
			return httpio.NewForbiddenMessagef("scope (%s), user (%s): the condition on (%s) on %s does not permit this row", scope, j.user, perm, checked)
		}
	}

	return nil
}

// composedItem is one composed row with what the rendering needs beside it.
type composedItem[Row any] struct {
	row   *Row
	image map[string]any
	caps  map[accesstypes.Permission]any
}

// capabilityAnswer is how a handler answers the capability envelope for one row, nil
// when the request asked for none.
type capabilityAnswer[Row any] func(ctx context.Context, item *composedItem[Row]) (map[accesstypes.Permission]any, error)

// collectComposed filters the composed items by the resource decision, runs the
// request's filter, sort and page over what is left, masks the cells the field decisions
// refuse, and attaches the envelope: the page's rows in the list's order, with the page
// whose headers the handler writes.
func collectComposed[Row Resourcer](ctx context.Context, cq *composedQuery[Row], judge *userJudge, items []*composedItem[Row], caps capabilityAnswer[Row]) ([]map[string]any, *Page[Row], error) {
	visible := make([]*composedItem[Row], 0, len(items))
	byRow := make(map[*Row]*composedItem[Row], len(items))
	for _, item := range items {
		ok, err := judge.permits(ctx, cq.resourceDecision, item.image, nil)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			visible = append(visible, item)
			byRow[item.row] = item
		}
	}

	page, err := cq.qSet.Collect(func(yield func(*Row, error) bool) {
		for _, item := range visible {
			if !yield(item.row, nil) {
				return
			}
		}
	})
	if err != nil {
		return nil, nil, err
	}

	resp := make([]map[string]any, 0, len(page.Rows()))
	for _, row := range page.Rows() {
		rmap, err := renderComposed(ctx, cq, judge, byRow[row], caps)
		if err != nil {
			return nil, nil, err
		}
		resp = append(resp, rmap)
	}
	if page.Reversed() {
		slices.Reverse(resp)
	}

	return resp, page, nil
}

// readComposed renders one composed item, or NotFound where there is none or the
// resource decision refuses it: a row the caller may not see does not exist for them.
func readComposed[Row Resourcer](ctx context.Context, cq *composedQuery[Row], judge *userJudge, item *composedItem[Row], label string, caps capabilityAnswer[Row]) (map[string]any, error) {
	if item != nil {
		ok, err := judge.permits(ctx, cq.resourceDecision, item.image, nil)
		if err != nil {
			return nil, err
		}
		if !ok {
			item = nil
		}
	}
	if item == nil {
		return nil, httpio.NewNotFoundMessagef("%s does not exist", label)
	}

	return renderComposed(ctx, cq, judge, item, caps)
}

// renderComposed renders one row the way the generated handlers render theirs: the
// projected fields by wire name, a cell whose field condition refuses the row left out,
// the envelope when the request asked for one.
func renderComposed[Row Resourcer](ctx context.Context, cq *composedQuery[Row], judge *userJudge, item *composedItem[Row], caps capabilityAnswer[Row]) (map[string]any, error) {
	value := reflect.ValueOf(item.row).Elem()
	rmap := make(map[string]any, len(cq.qSet.Fields()))
	for _, field := range cq.qSet.Fields() {
		if decision, conditional := cq.fieldDecisions[field]; conditional {
			ok, err := judge.permits(ctx, decision, item.image, nil)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
		}
		// The field name comes from the query set the decoder built over the resource's
		// own declared fields, never from the request as the caller typed it: a name the
		// resource does not declare was refused at decoding.
		// nosemgrep: go.lang.security.audit.unsafe-reflect-by-name.unsafe-reflect-by-name
		sf, ok := value.Type().FieldByName(string(field))
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(sf.Tag.Get(jsonTagKey), ",")
		if name == "" || name == "-" {
			continue
		}
		rmap[name] = value.FieldByIndex(sf.Index).Interface()
	}
	if len(cq.qSet.capabilities) > 0 && caps != nil {
		answer, err := caps(ctx, item)
		if err != nil {
			return nil, err
		}
		rmap[CapabilitiesProperty] = answer
	}

	return rmap, nil
}

// fieldResource names a field's resource the way a Set does: the resource and the
// field's wire name.
func fieldResource(res accesstypes.Resource, wire string) accesstypes.Resource {
	return accesstypes.Resource(string(res) + "." + wire)
}

// writeDecisions are the decisions the capability envelope answers from, checked once
// per request: Update on the resource and its writable fields, Delete on the resource,
// Execute on the methods that target it.
type writeDecisions struct {
	update  accesstypes.Decisions
	remove  accesstypes.Decisions
	execute accesstypes.Decisions
}

// capabilityDecisions checks what the request's envelope asks for.
func capabilityDecisions(ctx context.Context, userPermissions UserPermissions, scope accesstypes.Scope, asked []accesstypes.Permission, res accesstypes.Resource, fields []string, methods []accesstypes.Resource) (*writeDecisions, error) {
	env := RequestEnvironment()
	d := &writeDecisions{}
	var err error
	if slices.Contains(asked, accesstypes.Update) {
		resources := make([]accesstypes.Resource, 0, 1+len(fields))
		resources = append(resources, res)
		for _, field := range fields {
			resources = append(resources, fieldResource(res, field))
		}
		if d.update, err = userPermissions.Check(ctx, env, scope, accesstypes.Update, resources...); err != nil {
			return nil, errors.Wrap(err, "resource.UserPermissions.Check()")
		}
	}
	if slices.Contains(asked, accesstypes.Delete) {
		if d.remove, err = userPermissions.Check(ctx, env, scope, accesstypes.Delete, res); err != nil {
			return nil, errors.Wrap(err, "resource.UserPermissions.Check()")
		}
	}
	if slices.Contains(asked, accesstypes.Execute) && len(methods) > 0 {
		if d.execute, err = userPermissions.Check(ctx, env, scope, accesstypes.Execute, methods...); err != nil {
			return nil, errors.Wrap(err, "resource.UserPermissions.Check()")
		}
	}

	return d, nil
}

// envelope assembles one row's envelope from the decisions over its image: Update the
// editable fields (those the resource and the field permit, less the ones the rules
// withhold), Delete whether the delete is permitted and allowed, Execute the methods
// permitted and allowed.
func (d *writeDecisions) envelope(ctx context.Context, judge *userJudge, asked []accesstypes.Permission, res accesstypes.Resource, image map[string]any, fields []string, withheld func(field string) bool, deletable bool, methods []accesstypes.Resource, allowed func(method accesstypes.Resource) bool) (map[accesstypes.Permission]any, error) {
	answer := make(map[accesstypes.Permission]any, len(asked))
	for _, perm := range asked {
		switch perm {
		case accesstypes.Update:
			editable := make([]string, 0, len(fields))
			ok, err := judge.permits(ctx, d.update[res], image, nil)
			if err != nil {
				return nil, err
			}
			if ok {
				for _, field := range fields {
					permitted, err := judge.permits(ctx, d.update[fieldResource(res, field)], image, nil)
					if err != nil {
						return nil, err
					}
					if permitted && (withheld == nil || !withheld(field)) {
						editable = append(editable, field)
					}
				}
			}
			answer[perm] = editable
		case accesstypes.Delete:
			ok, err := judge.permits(ctx, d.remove[res], image, nil)
			if err != nil {
				return nil, err
			}
			answer[perm] = ok && deletable
		case accesstypes.Execute:
			runnable := make([]string, 0, len(methods))
			for _, method := range methods {
				ok, err := judge.permits(ctx, d.execute[method], image, nil)
				if err != nil {
					return nil, err
				}
				if ok && (allowed == nil || allowed(method)) {
					runnable = append(runnable, string(method))
				}
			}
			answer[perm] = runnable
		case accesstypes.Create:
			answer[perm] = []string{}
		case accesstypes.List, accesstypes.Read, accesstypes.NullPermission:
		}
	}

	return answer, nil
}
