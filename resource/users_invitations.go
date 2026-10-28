package resource

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/tracer"
	"github.com/cccteam/httpio"
	"github.com/cccteam/logger"
	"github.com/cccteam/session/sessioninfo"
	"github.com/go-playground/errors/v5"
)

// The invitations of the user surface: the first door into a tenant, and into the
// global partition. An invitation is a mailed link carrying the roles its acceptance
// grants (the session library's invitation): it reaches anyone by address, whether an
// account holds the address or not, and the actor's request is the same either way.
// Whoever follows the link and signs in, by any method, or signs up with a password,
// accepts it; the session library then runs the application's acceptance hook, which
// SeatInvited supplies over the Auth's membership store, once the transaction that
// consumed the invitation has committed. Each partition lists the invitations that stand into it,
// creates one with Create on <Auth>Invitations (<Auth><Tenant>Invitations), and
// withdraws one with Delete; an invitation is never updated.

// toInvitationRow types a composed invitation as the global partition's row.
func toInvitationRow[A UserAuth](c *composedInvitation) *InvitationRow[A] {
	return &InvitationRow[A]{ID: c.inv.ID, Email: c.inv.Email, Roles: slices.Clone(c.inv.Roles), InvitedBy: c.invitedBy(), CreatedAt: c.inv.CreatedAt, ExpiresAt: c.inv.ExpiresAt}
}

// toTenantInvitationRow types a composed invitation as the tenant partition's row.
func toTenantInvitationRow[A UserAuth](c *composedInvitation) *TenantInvitationRow[A] {
	return &TenantInvitationRow[A]{ID: c.inv.ID, Email: c.inv.Email, Roles: slices.Clone(c.inv.Roles), InvitedBy: c.invitedBy(), CreatedAt: c.inv.CreatedAt, ExpiresAt: c.inv.ExpiresAt}
}

// invitationItems composes the partition's invitation rows.
func invitationItems[Row Resourcer](ctx context.Context, rc *readContext, typed func(*composedInvitation) *Row, only func(*composedInvitation) bool) ([]*composedItem[Row], error) {
	invitations, err := rc.snap.invitationsIn(ctx)
	if err != nil {
		return nil, err
	}
	var items []*composedItem[Row]
	for _, c := range invitations {
		if only != nil && !only(c) {
			continue
		}
		items = append(items, &composedItem[Row]{row: typed(c), image: c.image()})
	}

	return items, nil
}

// invitationCapabilities answers the envelope of invitation rows: the delete alone, as
// the grant permits it over the row.
func invitationCapabilities[Row Resourcer](rc *readContext, asked []accesstypes.Permission) (capabilityAnswer[Row], error) {
	n := rc.partition.names
	decisions, err := capabilityDecisions(context.Background(), rc.perms, rc.partition.scope, asked, n.Invitations, nil, nil)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, item *composedItem[Row]) (map[accesstypes.Permission]any, error) {
		return decisions.envelope(ctx, rc.judge, asked, n.Invitations, item.image, nil, nil, true, nil, nil)
	}, nil
}

// listInvitations is the invitations list of a partition.
func listInvitations[A UserAuth, Row Resourcer](s *UserSurface[A], a UserApp, collection *GeneratedCollection, tenanted bool, typed func(*composedInvitation) *Row) http.HandlerFunc {
	decoder := MustNewQueryDecoder[Row, invitationRequest](collection, accesstypes.List).WithCursorKey(a.CursorKey()).WithPaging(invitationsOrder)

	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := tracer.Start(r.Context())
		defer span.End()

		partition, perms := s.partitionOf(r, tenanted), a.UserPermissions(r)
		cq, err := decodeComposed(decoder, r, perms, partition.scope, a.ResourceClient().DBType())
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		rc, err := s.readContext(ctx, partition, perms)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		items, err := invitationItems(ctx, rc, typed, nil)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		caps, err := invitationCapabilities[Row](rc, cq.qSet.capabilities)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}

		resp, page, err := collectComposed(ctx, cq, rc.judge, items, caps)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		if err := page.WriteHeaders(w, r); err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}

		return httpio.NewEncoder(w).Ok(resp)
	})
}

// readInvitation is the invitation read of a partition.
func readInvitation[A UserAuth, Row Resourcer](s *UserSurface[A], a UserApp, collection *GeneratedCollection, tenanted bool, typed func(*composedInvitation) *Row) http.HandlerFunc {
	decoder := MustNewQueryDecoder[Row, invitationRequest](collection, accesstypes.Read)
	param := s.names.Global.InvitationParam
	if tenanted {
		param = s.names.Tenant.InvitationParam
	}

	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := tracer.Start(r.Context())
		defer span.End()

		id := httpio.Param[ccc.UUID](r, param)
		partition, perms := s.partitionOf(r, tenanted), a.UserPermissions(r)
		cq, err := decodeComposed(decoder, r, perms, partition.scope, a.ResourceClient().DBType())
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		rc, err := s.readContext(ctx, partition, perms)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		items, err := invitationItems(ctx, rc, typed, func(c *composedInvitation) bool { return c.inv.ID == id })
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		caps, err := invitationCapabilities[Row](rc, cq.qSet.capabilities)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		var item *composedItem[Row]
		if len(items) > 0 {
			item = items[0]
		}

		rmap, err := readComposed(ctx, cq, rc.judge, item, "invitation "+id.String(), caps)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}

		return httpio.NewEncoder(w).Ok(rmap)
	})
}

// invitationWrite is one decoded invitation create: the fields it sets and their
// values.
type invitationWrite struct {
	fields []accesstypes.Field
	email  string
	roles  []string
}

// invitationWriteDecoder decodes an invitation operation's body for one partition's
// row type.
type invitationWriteDecoder interface {
	decode(op *Operation) (*invitationWrite, error)
}

type typedInvitationWriteDecoder[Row Resourcer] struct {
	decoder *Decoder[Row, invitationWriteRequest]
}

func newInvitationWriteDecoder[Row Resourcer](a UserApp) invitationWriteDecoder {
	rSet, err := NewSet[Row, invitationWriteRequest](accesstypes.Create, accesstypes.Delete)
	if err != nil {
		panic(err)
	}
	decoder, err := NewDecoder[Row, invitationWriteRequest](rSet)
	if err != nil {
		panic(err)
	}

	return &typedInvitationWriteDecoder[Row]{decoder: decoder.WithValidator(a.Validator())}
}

func (d *typedInvitationWriteDecoder[Row]) decode(op *Operation) (*invitationWrite, error) {
	patchSet, err := d.decoder.DecodeOperationWithoutPermissions(op)
	if err != nil {
		return nil, err
	}
	write := &invitationWrite{fields: patchSet.Fields()}
	if slices.Contains(write.fields, userFieldEmail) {
		write.email, _ = patchSet.Get(userFieldEmail).(string)
	}
	if slices.Contains(write.fields, invitationFieldRoles) {
		write.roles, _ = patchSet.Get(invitationFieldRoles).([]string)
	}

	return write, nil
}

// invitationFieldRoles is the Go field name of an invitation's roles.
const invitationFieldRoles accesstypes.Field = "Roles"

// invitationsPatch is the invitations patch handler of a partition: invite, revoke; an
// invitation is never updated.
func (s *UserSurface[A]) invitationsPatch(a UserApp, tenanted bool) http.HandlerFunc {
	var decoder invitationWriteDecoder
	if tenanted {
		decoder = newInvitationWriteDecoder[TenantInvitationRow[A]](a)
	} else {
		decoder = newInvitationWriteDecoder[InvitationRow[A]](a)
	}

	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := tracer.Start(r.Context())
		defer span.End()

		wc := s.writeContext(r, a, tenanted)
		resp := userWriteResponse{IDs: []ccc.UUID{}}
		for op, err := range Operations(r, userOperationPath) {
			if err != nil {
				return httpio.NewEncoder(w).ClientMessage(ctx, errors.Wrap(err, "resource.Operations()"))
			}
			switch op.Type {
			case OperationCreate:
				write, err := decoder.decode(op)
				if err != nil {
					return httpio.NewEncoder(w).ClientMessage(ctx, err)
				}
				id, err := s.createInvitation(ctx, wc, write)
				if err != nil {
					return httpio.NewEncoder(w).ClientMessage(ctx, err)
				}
				resp.IDs = append(resp.IDs, id)
			case OperationUpdate:
				return httpio.NewEncoder(w).ClientMessage(ctx, httpio.NewBadRequestMessage("an invitation is not updated: revoke it and invite again"))
			case OperationDelete:
				if err := s.revokeInvitation(ctx, wc, op); err != nil {
					return httpio.NewEncoder(w).ClientMessage(ctx, err)
				}
			}
		}
		if wc.dryRun {
			return httpio.NewEncoder(w).Ok(nil)
		}

		return httpio.NewEncoder(w).Ok(resp)
	})
}

// createInvitation invites an address to hold roles in the partition: the roles must
// exist where the partition seats, and the grant's condition is judged over the
// proposed row once per role, so a delegate invites into no role they could not seat.
// The invitation's tenant is the partition's; the caller is recorded as its sender.
func (s *UserSurface[A]) createInvitation(ctx context.Context, wc *writeContext, write *invitationWrite) (ccc.UUID, error) {
	n := wc.partition.names
	fields := make([]accesstypes.Resource, 0, len(write.fields))
	for _, field := range write.fields {
		fields = append(fields, fieldResource(n.Invitations, strings.ToLower(string(field[:1]))+string(field[1:])))
	}
	decisions, err := checkWriteGrants(ctx, wc.perms, wc.partition.scope, accesstypes.Create, n.Invitations, fields)
	if err != nil {
		return ccc.NilUUID, err
	}
	if err := wc.ensure(ctx); err != nil {
		return ccc.NilUUID, err
	}
	email := strings.TrimSpace(write.email)
	if email == "" {
		return ccc.NilUUID, httpio.NewBadRequestMessage("an address is required: the invitation is mailed to it")
	}
	if len(write.roles) == 0 {
		return ccc.NilUUID, httpio.NewBadRequestMessage("at least one role is required: the roles are what the invitation grants")
	}
	roles := make([]string, 0, len(write.roles))
	for _, role := range write.roles {
		role = strings.TrimSpace(role)
		if slices.Contains(roles, role) {
			continue
		}
		if _, ok := wc.snap.roleScope(accesstypes.Role(role)); !ok {
			return ccc.NilUUID, httpio.NewBadRequestMessagef("role %q does not exist in %s", role, wc.partition.scope)
		}
		roles = append(roles, role)
	}
	caller := wc.snap.byUsername(string(wc.perms.User()))
	post := map[string]any{
		userAttrEmail:         email,
		seatAttrRole:          slices.Clone(roles),
		invitationAttrInvited: string(wc.perms.User()),
		invitationAttrCreated: time.Now(),
		invitationAttrExpires: nil,
	}
	if err := wc.judge.judgeWrite(ctx, decisions, wc.partition.scope, accesstypes.Create, nil, post); err != nil {
		return ccc.NilUUID, err
	}
	if wc.dryRun {
		return ccc.NilUUID, nil
	}
	req := &UserInvitationRequest{Email: email, Roles: roles}
	if n.Tenanted {
		req.Tenant = string(wc.partition.domain)
	}
	if caller != nil {
		req.InvitedBy = ccc.NullUUID{UUID: caller.ID, Valid: true}
	}
	inv, err := s.accounts.Invite(ctx, req)
	if err != nil {
		return ccc.NilUUID, errors.Wrap(err, "resource.UserAccounts.Invite()")
	}

	return inv.ID, nil
}

// revokeInvitation withdraws an invitation of the partition: NotFound where it stands
// in none, or in another partition.
func (s *UserSurface[A]) revokeInvitation(ctx context.Context, wc *writeContext, op *Operation) error {
	n := wc.partition.names
	decisions, err := checkWriteGrants(ctx, wc.perms, wc.partition.scope, accesstypes.Delete, n.Invitations, nil)
	if err != nil {
		return err
	}
	if err := wc.ensure(ctx); err != nil {
		return err
	}
	id, err := operationUUID(op, "id")
	if err != nil {
		return err
	}
	invitations, err := wc.snap.invitationsIn(ctx)
	if err != nil {
		return err
	}
	var target *composedInvitation
	for _, c := range invitations {
		if c.inv.ID == id {
			target = c
		}
	}
	if target == nil {
		return httpio.NewNotFoundMessagef("invitation %s does not exist", id)
	}
	if err := wc.judge.judgeWrite(ctx, decisions, wc.partition.scope, accesstypes.Delete, target.image(), nil); err != nil {
		return err
	}
	if wc.dryRun {
		return nil
	}
	if err := s.accounts.RevokeInvitation(ctx, id); err != nil {
		return errors.Wrap(err, "resource.UserAccounts.RevokeInvitation()")
	}

	return nil
}

// SeatInvited is the session library's acceptance hook (session.Auth.SetInvitationAccepted)
// over the Auth's membership store: when a person accepts an invitation, by signing in
// or signing up at it, the roles it carries are written as seats for them, in the
// invitation's tenant, or, for an invitation into the global partition, where each role
// is held (the global scope for a role of the global kind, every tenant for one of the
// domain kind), exactly as the surface seats by hand. The library runs the hook once
// the transaction that consumed the invitation has committed, outside any transaction,
// so the engine's own client writes the memberships; the writes are idempotent, so a
// sign-in that fails after them leaves seats a later seat of the same roles reuses.
func SeatInvited(seats UserSeats) func(ctx context.Context, userID ccc.UUID, username string, inv *sessioninfo.Invitation) error {
	return func(ctx context.Context, _ ccc.UUID, username string, inv *sessioninfo.Invitation) error {
		byScope, err := invitedScopes(ctx, seats, inv)
		if err != nil {
			return err
		}
		for scope, roles := range byScope {
			if err := seats.AddUserRoles(ctx, scope, userKey(username), roles...); err != nil {
				return errors.Wrapf(err, "resource.UserSeats.AddUserRoles(): seating %q in %s", username, scope)
			}
		}
		logger.FromCtx(ctx).Infof("user surface: %q accepted the invitation to %s and holds %s", username, inv.Email, strings.Join(inv.RoleNames, ", "))

		return nil
	}
}

// invitedScopes places an invitation's roles where they are held: the tenant's scope
// for a tenant invitation; for a global one, the global scope or every tenant by the
// role's kind. A role that exists in neither is an error, since the invitation was
// written against the roles of its day and the role file has since changed.
func invitedScopes(ctx context.Context, seats UserSeats, inv *sessioninfo.Invitation) (map[accesstypes.PolicyScope][]accesstypes.Role, error) {
	byScope := make(map[accesstypes.PolicyScope][]accesstypes.Role, 2)
	if inv.Tenant != "" {
		scope := accesstypes.DomainPolicyScope(accesstypes.Domain(inv.Tenant))
		for _, role := range inv.RoleNames {
			byScope[scope] = append(byScope[scope], accesstypes.Role(role))
		}

		return byScope, nil
	}
	candidates := []accesstypes.PolicyScope{accesstypes.GlobalPolicyScope(), accesstypes.EveryDomainPolicyScope()}
	existing := make(map[accesstypes.PolicyScope][]access.RoleInfo, len(candidates))
	for _, scope := range candidates {
		roles, err := seats.Roles(ctx, scope)
		if err != nil {
			return nil, errors.Wrap(err, "resource.UserSeats.Roles()")
		}
		existing[scope] = roles
	}
	for _, name := range inv.RoleNames {
		role := accesstypes.Role(name)
		placed := false
		for _, scope := range candidates {
			if roleListed(existing[scope], role) {
				byScope[scope] = append(byScope[scope], role)
				placed = true

				break
			}
		}
		if !placed {
			return nil, errors.Newf("the invitation to %s names role %q, which exists neither globally nor in every tenant", inv.Email, name)
		}
	}

	return byScope, nil
}
