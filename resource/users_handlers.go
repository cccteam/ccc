package resource

import (
	"context"
	"net/http"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/tracer"
	"github.com/cccteam/httpio"
)

// The handlers of the user surface, one per route the generated router mounts: the
// global partition's under the outlet's prefix, the tenant partition's under the domain
// segment. Each generated handler delegates to one of these with the application's
// decoder seam and the generated collection.

// partitionOf is the partition a request runs in: the tenant partition reads the tenant
// from the route.
func (s *UserSurface[A]) partitionOf(r *http.Request, tenanted bool) *userPartition {
	if !tenanted {
		return s.globalPartition()
	}
	s.requireTenant()

	return s.tenantPartition(httpio.Param[accesstypes.Domain](r, s.decl.DomainParam))
}

// The editable fields of a user row, and the methods that target it.
var userEditableFields = []string{userAttrUsername, userAttrDisabled}

// Users serves the global partition's users list: List on <Auth>Users.
func (s *UserSurface[A]) Users(a UserApp, collection *GeneratedCollection) http.HandlerFunc {
	return listUsers(s, a, collection, false, toUserRow[A])
}

// User serves one user of the global partition: Read on <Auth>Users.
func (s *UserSurface[A]) User(a UserApp, collection *GeneratedCollection) http.HandlerFunc {
	return readUser(s, a, collection, false, toUserRow[A])
}

// PatchUsers serves the global partition's user writes: Create, Update and Delete on
// <Auth>Users through the operations body.
func (s *UserSurface[A]) PatchUsers(a UserApp, _ *GeneratedCollection) http.HandlerFunc {
	return s.usersPatch(a, false)
}

// TenantUsers serves the tenant partition's users list: List on <Auth><Tenant>Users in
// the route's tenant.
func (s *UserSurface[A]) TenantUsers(a UserApp, collection *GeneratedCollection) http.HandlerFunc {
	s.requireTenant()

	return listUsers(s, a, collection, true, toTenantUserRow[A])
}

// TenantUser serves one user of the tenant partition: Read on <Auth><Tenant>Users.
func (s *UserSurface[A]) TenantUser(a UserApp, collection *GeneratedCollection) http.HandlerFunc {
	s.requireTenant()

	return readUser(s, a, collection, true, toTenantUserRow[A])
}

// PatchTenantUsers serves the tenant partition's user writes.
func (s *UserSurface[A]) PatchTenantUsers(a UserApp, _ *GeneratedCollection) http.HandlerFunc {
	s.requireTenant()

	return s.usersPatch(a, true)
}

// Seats serves the global partition's seats list: List on <Auth>Seats.
func (s *UserSurface[A]) Seats(a UserApp, collection *GeneratedCollection) http.HandlerFunc {
	return listSeats(s, a, collection, false, toSeatRow[A])
}

// Seat serves one seat of the global partition: Read on <Auth>Seats.
func (s *UserSurface[A]) Seat(a UserApp, collection *GeneratedCollection) http.HandlerFunc {
	return readSeat(s, a, collection, false, toSeatRow[A])
}

// PatchSeats serves the global partition's seat writes: Create and Delete on <Auth>Seats.
func (s *UserSurface[A]) PatchSeats(a UserApp, _ *GeneratedCollection) http.HandlerFunc {
	return s.seatsPatch(a, false)
}

// TenantSeats serves the tenant partition's seats list.
func (s *UserSurface[A]) TenantSeats(a UserApp, collection *GeneratedCollection) http.HandlerFunc {
	s.requireTenant()

	return listSeats(s, a, collection, true, toTenantSeatRow[A])
}

// TenantSeat serves one seat of the tenant partition.
func (s *UserSurface[A]) TenantSeat(a UserApp, collection *GeneratedCollection) http.HandlerFunc {
	s.requireTenant()

	return readSeat(s, a, collection, true, toTenantSeatRow[A])
}

// PatchTenantSeats serves the tenant partition's seat writes.
func (s *UserSurface[A]) PatchTenantSeats(a UserApp, _ *GeneratedCollection) http.HandlerFunc {
	s.requireTenant()

	return s.seatsPatch(a, true)
}

// Roles serves the global partition's roles: List on <Auth>Roles.
func (s *UserSurface[A]) Roles(a UserApp, collection *GeneratedCollection) http.HandlerFunc {
	return listRoles(s, a, collection, false, toRoleRow[A])
}

// Role serves one role of the global partition: Read on <Auth>Roles.
func (s *UserSurface[A]) Role(a UserApp, collection *GeneratedCollection) http.HandlerFunc {
	return readRole(s, a, collection, false, toRoleRow[A])
}

// TenantRoles serves the tenant partition's roles.
func (s *UserSurface[A]) TenantRoles(a UserApp, collection *GeneratedCollection) http.HandlerFunc {
	s.requireTenant()

	return listRoles(s, a, collection, true, toTenantRoleRow[A])
}

// TenantRole serves one role of the tenant partition.
func (s *UserSurface[A]) TenantRole(a UserApp, collection *GeneratedCollection) http.HandlerFunc {
	s.requireTenant()

	return readRole(s, a, collection, true, toTenantRoleRow[A])
}

// SetUserPassword serves Set<Auth>UserPassword: Execute, targeting <Auth>Users.
func (s *UserSurface[A]) SetUserPassword(a UserApp, _ *GeneratedCollection) http.HandlerFunc {
	return s.setUserPassword(a, false)
}

// UnlinkUserIdentity serves Unlink<Auth>UserIdentity: Execute, targeting <Auth>Users.
func (s *UserSurface[A]) UnlinkUserIdentity(a UserApp, _ *GeneratedCollection) http.HandlerFunc {
	return s.unlinkUserIdentity(a, false)
}

// SetTenantUserPassword serves Set<Auth><Tenant>UserPassword.
func (s *UserSurface[A]) SetTenantUserPassword(a UserApp, _ *GeneratedCollection) http.HandlerFunc {
	s.requireTenant()

	return s.setUserPassword(a, true)
}

// UnlinkTenantUserIdentity serves Unlink<Auth><Tenant>UserIdentity.
func (s *UserSurface[A]) UnlinkTenantUserIdentity(a UserApp, _ *GeneratedCollection) http.HandlerFunc {
	s.requireTenant()

	return s.unlinkUserIdentity(a, true)
}

// readContext is what a list or read composes once: the partition, the snapshot and
// the judge.
type readContext struct {
	partition *userPartition
	snap      *userSnapshot
	judge     *userJudge
	perms     UserPermissions
}

func (s *UserSurface[A]) readContext(ctx context.Context, r *http.Request, a UserApp, tenanted bool) (*readContext, error) {
	partition := s.partitionOf(r, tenanted)
	snap, err := s.snapshot(ctx, partition)
	if err != nil {
		return nil, err
	}
	perms := a.UserPermissions(r)

	return &readContext{partition: partition, snap: snap, judge: newUserJudge(snap, perms), perms: perms}, nil
}

// userItems composes the partition's user rows.
func userItems[Row Resourcer](ctx context.Context, rc *readContext, typed func(*composedUser) *Row, only func(*UserAccount) bool) ([]*composedItem[Row], []*composedUser, error) {
	var items []*composedItem[Row]
	var users []*composedUser
	for i := range rc.snap.users {
		account := &rc.snap.users[i]
		if !rc.snap.inPartition(account.Username) || (only != nil && !only(account)) {
			continue
		}
		user, err := rc.snap.userRowOf(ctx, account)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, &composedItem[Row]{row: typed(user), image: user.image()})
		users = append(users, user)
	}

	return items, users, nil
}

// userCapabilities answers the envelope of user rows: the editable fields less
// disabled on one's own row, the delete less one's own row, the methods less the
// password reset on one's own row; in the tenant partition a user seated outside the
// tenant has no account-level affordance.
func userCapabilities[A UserAuth, Row Resourcer](s *UserSurface[A], rc *readContext, asked []accesstypes.Permission, users []*composedUser, items []*composedItem[Row]) (capabilityAnswer[Row], error) {
	n := rc.partition.names
	methods := n.servedMethods(s.decl.Shape)
	decisions, err := capabilityDecisions(context.Background(), rc.perms, rc.partition.scope, asked, n.Users, userEditableFields, methods)
	if err != nil {
		return nil, err
	}
	byItem := make(map[*composedItem[Row]]*composedUser, len(items))
	for i := range items {
		byItem[items[i]] = users[i]
	}

	return func(ctx context.Context, item *composedItem[Row]) (map[accesstypes.Permission]any, error) {
		user := byItem[item]
		self := rc.judge.self(user.account.Username)
		outside := false
		if n.Tenanted {
			wc := &writeContext{partition: rc.partition, snap: rc.snap, judge: rc.judge, perms: rc.perms}
			if outside, err = s.seatedOutside(ctx, wc, user.account.Username); err != nil {
				return nil, err
			}
		}
		withheld := func(field string) bool {
			return outside || (self && field == userAttrDisabled)
		}
		allowed := func(method accesstypes.Resource) bool {
			return !outside && (!self || method != n.SetPassword)
		}

		return decisions.envelope(ctx, rc.judge, asked, n.Users, item.image, userEditableFields, withheld, !self && !outside, methods, allowed)
	}, nil
}

// listUsers is the users list of a partition.
func listUsers[A UserAuth, Row Resourcer](s *UserSurface[A], a UserApp, collection *GeneratedCollection, tenanted bool, typed func(*composedUser) *Row) http.HandlerFunc {
	decoder := MustNewQueryDecoder[Row, userRequest](collection, accesstypes.List).WithCursorKey(a.CursorKey()).WithPaging(usersOrder)

	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := tracer.Start(r.Context())
		defer span.End()

		rc, err := s.readContext(ctx, r, a, tenanted)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		cq, err := decodeComposed(decoder, r, rc.perms, rc.partition.scope, a.ResourceClient().DBType())
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		items, users, err := userItems(ctx, rc, typed, nil)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		caps, err := userCapabilities(s, rc, cq.qSet.capabilities, users, items)
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

// readUser is the user read of a partition.
func readUser[A UserAuth, Row Resourcer](s *UserSurface[A], a UserApp, collection *GeneratedCollection, tenanted bool, typed func(*composedUser) *Row) http.HandlerFunc {
	decoder := MustNewQueryDecoder[Row, userRequest](collection, accesstypes.Read)
	param := s.names.Global.UserParam
	if tenanted {
		param = s.names.Tenant.UserParam
	}

	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := tracer.Start(r.Context())
		defer span.End()

		id := httpio.Param[ccc.UUID](r, param)
		rc, err := s.readContext(ctx, r, a, tenanted)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		cq, err := decodeComposed(decoder, r, rc.perms, rc.partition.scope, a.ResourceClient().DBType())
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		items, users, err := userItems(ctx, rc, typed, func(account *UserAccount) bool { return account.ID == id })
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		caps, err := userCapabilities(s, rc, cq.qSet.capabilities, users, items)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		var item *composedItem[Row]
		if len(items) > 0 {
			item = items[0]
		}

		rmap, err := readComposed(ctx, cq, rc.judge, item, "user "+id.String(), caps)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}

		return httpio.NewEncoder(w).Ok(rmap)
	})
}

// toSeatRow types a composed seat as the global partition's row.
func toSeatRow[A UserAuth](seat *composedSeat) *SeatRow[A] {
	return &SeatRow[A]{User: seat.user.ID, Role: string(seat.role), Username: seat.user.Username, EveryDomain: seat.everywhere()}
}

// toTenantSeatRow types a composed seat as the tenant partition's row.
func toTenantSeatRow[A UserAuth](seat *composedSeat) *TenantSeatRow[A] {
	return &TenantSeatRow[A]{User: seat.user.ID, Role: string(seat.role), Username: seat.user.Username, Everywhere: seat.everywhere()}
}

// seatItems composes the partition's seat rows.
func seatItems[Row Resourcer](rc *readContext, typed func(*composedSeat) *Row, only func(*composedSeat) bool) ([]*composedItem[Row], []*composedSeat) {
	var items []*composedItem[Row]
	var seats []*composedSeat
	for _, seat := range rc.snap.seatsOf() {
		if only != nil && !only(seat) {
			continue
		}
		items = append(items, &composedItem[Row]{row: typed(seat), image: seat.image(rc.partition.names.Tenanted)})
		seats = append(seats, seat)
	}

	return items, seats
}

// seatCapabilities answers the envelope of seat rows: the delete alone, withheld on
// one's own seats, a directory's seats, and a seat held everywhere shown in a tenant.
func seatCapabilities[Row Resourcer](rc *readContext, asked []accesstypes.Permission, seats []*composedSeat, items []*composedItem[Row]) (capabilityAnswer[Row], error) {
	n := rc.partition.names
	decisions, err := capabilityDecisions(context.Background(), rc.perms, rc.partition.scope, asked, n.Seats, nil, nil)
	if err != nil {
		return nil, err
	}
	byItem := make(map[*composedItem[Row]]*composedSeat, len(items))
	for i := range items {
		byItem[items[i]] = seats[i]
	}

	return func(ctx context.Context, item *composedItem[Row]) (map[accesstypes.Permission]any, error) {
		seat := byItem[item]
		facts, err := rc.snap.factsOf(ctx, seat.user)
		if err != nil {
			return nil, err
		}
		deletable := !rc.judge.self(seat.user.Username) && !facts.rolesSynced && (!n.Tenanted || !seat.everywhere())

		return decisions.envelope(ctx, rc.judge, asked, n.Seats, item.image, nil, nil, deletable, nil, nil)
	}, nil
}

// listSeats is the seats list of a partition.
func listSeats[A UserAuth, Row Resourcer](s *UserSurface[A], a UserApp, collection *GeneratedCollection, tenanted bool, typed func(*composedSeat) *Row) http.HandlerFunc {
	var decoder *QueryDecoder[Row, seatRequest]
	var tenantDecoder *QueryDecoder[Row, tenantSeatRequest]
	if tenanted {
		tenantDecoder = MustNewQueryDecoder[Row, tenantSeatRequest](collection, accesstypes.List).WithCursorKey(a.CursorKey()).WithPaging(seatsOrder)
	} else {
		decoder = MustNewQueryDecoder[Row, seatRequest](collection, accesstypes.List).WithCursorKey(a.CursorKey()).WithPaging(seatsOrder)
	}

	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := tracer.Start(r.Context())
		defer span.End()

		rc, err := s.readContext(ctx, r, a, tenanted)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		var cq *composedQuery[Row]
		if tenanted {
			cq, err = decodeComposed(tenantDecoder, r, rc.perms, rc.partition.scope, a.ResourceClient().DBType())
		} else {
			cq, err = decodeComposed(decoder, r, rc.perms, rc.partition.scope, a.ResourceClient().DBType())
		}
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		items, seats := seatItems(rc, typed, nil)
		caps, err := seatCapabilities(rc, cq.qSet.capabilities, seats, items)
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

// readSeat is the seat read of a partition, addressed by user and role.
func readSeat[A UserAuth, Row Resourcer](s *UserSurface[A], a UserApp, collection *GeneratedCollection, tenanted bool, typed func(*composedSeat) *Row) http.HandlerFunc {
	var decoder *QueryDecoder[Row, seatRequest]
	var tenantDecoder *QueryDecoder[Row, tenantSeatRequest]
	names := s.names.Global
	if tenanted {
		names = s.names.Tenant
		tenantDecoder = MustNewQueryDecoder[Row, tenantSeatRequest](collection, accesstypes.Read)
	} else {
		decoder = MustNewQueryDecoder[Row, seatRequest](collection, accesstypes.Read)
	}

	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := tracer.Start(r.Context())
		defer span.End()

		user := httpio.Param[ccc.UUID](r, names.SeatUserParam)
		role := accesstypes.Role(httpio.Param[string](r, names.SeatRoleParam))
		rc, err := s.readContext(ctx, r, a, tenanted)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		var cq *composedQuery[Row]
		if tenanted {
			cq, err = decodeComposed(tenantDecoder, r, rc.perms, rc.partition.scope, a.ResourceClient().DBType())
		} else {
			cq, err = decodeComposed(decoder, r, rc.perms, rc.partition.scope, a.ResourceClient().DBType())
		}
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		items, seats := seatItems(rc, typed, func(seat *composedSeat) bool { return seat.user.ID == user && seat.role == role })
		caps, err := seatCapabilities(rc, cq.qSet.capabilities, seats, items)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		var item *composedItem[Row]
		if len(items) > 0 {
			item = items[0]
		}

		rmap, err := readComposed(ctx, cq, rc.judge, item, "seat "+user.String()+" "+string(role), caps)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}

		return httpio.NewEncoder(w).Ok(rmap)
	})
}

// toRoleRow types a composed role as the global partition's row.
func toRoleRow[A UserAuth](role *composedRole) *RoleRow[A] {
	return &RoleRow[A]{Name: string(role.name), Kind: role.kind()}
}

// toTenantRoleRow types a composed role as the tenant partition's row.
func toTenantRoleRow[A UserAuth](role *composedRole) *TenantRoleRow[A] {
	return &TenantRoleRow[A]{Name: string(role.name)}
}

// roleItems composes the partition's role rows.
func roleItems[Row Resourcer](rc *readContext, typed func(*composedRole) *Row, only func(*composedRole) bool) []*composedItem[Row] {
	var items []*composedItem[Row]
	for _, role := range rc.snap.rolesOf() {
		if only != nil && !only(role) {
			continue
		}
		items = append(items, &composedItem[Row]{row: typed(role), image: role.image(rc.partition.names.Tenanted)})
	}

	return items
}

// roleCapabilities answers the envelope of role rows: nothing is written on a role here.
func roleCapabilities[Row Resourcer](asked []accesstypes.Permission) capabilityAnswer[Row] {
	return func(_ context.Context, _ *composedItem[Row]) (map[accesstypes.Permission]any, error) {
		answer := make(map[accesstypes.Permission]any, len(asked))
		for _, perm := range asked {
			if perm == accesstypes.Delete {
				answer[perm] = false
			} else {
				answer[perm] = []string{}
			}
		}

		return answer, nil
	}
}

// listRoles is the roles list of a partition.
func listRoles[A UserAuth, Row Resourcer](s *UserSurface[A], a UserApp, collection *GeneratedCollection, tenanted bool, typed func(*composedRole) *Row) http.HandlerFunc {
	var decoder *QueryDecoder[Row, roleRequest]
	var tenantDecoder *QueryDecoder[Row, tenantRoleRequest]
	if tenanted {
		tenantDecoder = MustNewQueryDecoder[Row, tenantRoleRequest](collection, accesstypes.List).WithCursorKey(a.CursorKey()).WithPaging(rolesOrder)
	} else {
		decoder = MustNewQueryDecoder[Row, roleRequest](collection, accesstypes.List).WithCursorKey(a.CursorKey()).WithPaging(rolesOrder)
	}

	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := tracer.Start(r.Context())
		defer span.End()

		rc, err := s.readContext(ctx, r, a, tenanted)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		var cq *composedQuery[Row]
		if tenanted {
			cq, err = decodeComposed(tenantDecoder, r, rc.perms, rc.partition.scope, a.ResourceClient().DBType())
		} else {
			cq, err = decodeComposed(decoder, r, rc.perms, rc.partition.scope, a.ResourceClient().DBType())
		}
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}

		resp, page, err := collectComposed(ctx, cq, rc.judge, roleItems(rc, typed, nil), roleCapabilities[Row](cq.qSet.capabilities))
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		if err := page.WriteHeaders(w, r); err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}

		return httpio.NewEncoder(w).Ok(resp)
	})
}

// readRole is the role read of a partition.
func readRole[A UserAuth, Row Resourcer](s *UserSurface[A], a UserApp, collection *GeneratedCollection, tenanted bool, typed func(*composedRole) *Row) http.HandlerFunc {
	var decoder *QueryDecoder[Row, roleRequest]
	var tenantDecoder *QueryDecoder[Row, tenantRoleRequest]
	names := s.names.Global
	if tenanted {
		names = s.names.Tenant
		tenantDecoder = MustNewQueryDecoder[Row, tenantRoleRequest](collection, accesstypes.Read)
	} else {
		decoder = MustNewQueryDecoder[Row, roleRequest](collection, accesstypes.Read)
	}

	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := tracer.Start(r.Context())
		defer span.End()

		name := accesstypes.Role(httpio.Param[string](r, names.RoleParam))
		rc, err := s.readContext(ctx, r, a, tenanted)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		var cq *composedQuery[Row]
		if tenanted {
			cq, err = decodeComposed(tenantDecoder, r, rc.perms, rc.partition.scope, a.ResourceClient().DBType())
		} else {
			cq, err = decodeComposed(decoder, r, rc.perms, rc.partition.scope, a.ResourceClient().DBType())
		}
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		items := roleItems(rc, typed, func(role *composedRole) bool { return role.name == name })
		var item *composedItem[Row]
		if len(items) > 0 {
			item = items[0]
		}

		rmap, err := readComposed(ctx, cq, rc.judge, item, "role "+string(name), roleCapabilities[Row](cq.qSet.capabilities))
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}

		return httpio.NewEncoder(w).Ok(rmap)
	})
}
