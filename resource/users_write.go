package resource

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/tracer"
	"github.com/cccteam/httpio"
	"github.com/cccteam/logger"
	"github.com/go-playground/errors/v5"
)

// The user surface's writes. Each partition's users and seats take the operations body
// every generated resource takes (resource.Operations): add, patch and remove, applied
// in order. The stores behind the rows are two, so the operations of one body are not
// one transaction: the first that fails answers its error and the ones before it stand.
// A dry run (X-Dry-Run) checks and refuses exactly as the real call does and applies
// nothing.

// userWriteResponse is what a users patch answers: the ids of the users it created, as a
// generated resource with a generated key answers.
type userWriteResponse struct {
	IDs []ccc.UUID `json:"iDs"`
}

// The operation path patterns: a user is addressed by its id, a seat by its user and
// role.
const (
	userOperationPath = "/{id}"
	seatOperationPath = "/{id1}/{id2}"
)

// writeContext is what one write knows: the request's partition and caller, the
// snapshot it judges against, and whether it applies anything.
type writeContext struct {
	partition *userPartition
	snap      *userSnapshot
	judge     *userJudge
	perms     UserPermissions
	dryRun    bool
}

// usersPatch is the users patch handler of a partition.
func (s *UserSurface[A]) usersPatch(a UserApp, tenanted bool) http.HandlerFunc {
	var decoder userWriteDecoder
	if tenanted {
		decoder = newUserWriteDecoder[TenantUserRow[A], tenantUserWriteRequest](a)
	} else {
		decoder = newUserWriteDecoder[UserRow[A], userWriteRequest](a)
	}

	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := tracer.Start(r.Context())
		defer span.End()

		wc, err := s.writeContext(ctx, r, a, tenanted)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		resp := userWriteResponse{IDs: []ccc.UUID{}}
		for op, err := range Operations(r, userOperationPath) {
			if err != nil {
				return httpio.NewEncoder(w).ClientMessage(ctx, errors.Wrap(err, "resource.Operations()"))
			}
			write, err := decoder.decode(op)
			if err != nil {
				return httpio.NewEncoder(w).ClientMessage(ctx, err)
			}
			switch op.Type {
			case OperationCreate:
				id, err := s.createUser(ctx, wc, write)
				if err != nil {
					return httpio.NewEncoder(w).ClientMessage(ctx, err)
				}
				resp.IDs = append(resp.IDs, id)
			case OperationUpdate:
				if err := s.updateUser(ctx, wc, httpio.Param[ccc.UUID](op.Req, "id"), write); err != nil {
					return httpio.NewEncoder(w).ClientMessage(ctx, err)
				}
			case OperationDelete:
				if err := s.deleteUser(ctx, wc, httpio.Param[ccc.UUID](op.Req, "id")); err != nil {
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

// writeContext composes what the writes of one request share.
func (s *UserSurface[A]) writeContext(ctx context.Context, r *http.Request, a UserApp, tenanted bool) (*writeContext, error) {
	partition := s.partitionOf(r, tenanted)
	snap, err := s.snapshot(ctx, partition)
	if err != nil {
		return nil, err
	}
	perms := a.UserPermissions(r)

	return &writeContext{partition: partition, snap: snap, judge: newUserJudge(snap, perms), perms: perms, dryRun: IsDryRun(r)}, nil
}

// userWrite is one decoded user operation: the fields it sets and their values.
type userWrite struct {
	fields   []accesstypes.Field
	username string
	password *string
	disabled bool
	role     string
}

func (w *userWrite) sets(field accesstypes.Field) bool {
	return slices.Contains(w.fields, field)
}

// userWriteDecoder decodes a user operation's body for one partition's write shape.
type userWriteDecoder interface {
	decode(op *Operation) (*userWrite, error)
}

type typedUserWriteDecoder[Row Resourcer, Request any] struct {
	decoder *Decoder[Row, Request]
}

func newUserWriteDecoder[Row Resourcer, Request any](a UserApp) userWriteDecoder {
	rSet, err := NewSet[Row, Request](accesstypes.Create, accesstypes.Update, accesstypes.Delete)
	if err != nil {
		panic(err)
	}
	decoder, err := NewDecoder[Row, Request](rSet)
	if err != nil {
		panic(err)
	}

	return &typedUserWriteDecoder[Row, Request]{decoder: decoder.WithValidator(a.Validator())}
}

func (d *typedUserWriteDecoder[Row, Request]) decode(op *Operation) (*userWrite, error) {
	patchSet, err := d.decoder.DecodeOperationWithoutPermissions(op)
	if err != nil {
		return nil, err
	}
	write := &userWrite{fields: patchSet.Fields()}
	if write.sets(userFieldUsername) {
		write.username, _ = patchSet.Get(userFieldUsername).(string)
	}
	if write.sets(userFieldPassword) {
		write.password, _ = patchSet.Get(userFieldPassword).(*string)
	}
	if write.sets(userFieldDisabled) {
		write.disabled, _ = patchSet.Get(userFieldDisabled).(bool)
	}
	if write.sets(userFieldRole) {
		write.role, _ = patchSet.Get(userFieldRole).(string)
	}

	return write, nil
}

// createUser adds a user: the shape decides whether the surface creates users and what it
// does with a password; in the tenant partition the create also seats the user there,
// which needs Create on the tenant's seats too. The permission check runs over the
// proposed row.
func (s *UserSurface[A]) createUser(ctx context.Context, wc *writeContext, write *userWrite) (ccc.UUID, error) {
	n := wc.partition.names
	if !s.decl.Shape.CreatesUsers() {
		return ccc.NilUUID, httpio.NewBadRequestMessagef("the %s auth creates no users: its people are created by their first sign-in through the directory", s.decl.Auth)
	}
	username := strings.TrimSpace(write.username)
	if username == "" {
		return ccc.NilUUID, httpio.NewBadRequestMessage("a username is required")
	}
	switch s.decl.Shape.passwordOnCreate() {
	case passwordRequired:
		if write.password == nil || *write.password == "" {
			return ccc.NilUUID, httpio.NewBadRequestMessage("a password is required: the password is the way in")
		}
	case passwordRefused:
		if write.password != nil {
			return ccc.NilUUID, httpio.NewBadRequestMessagef("the %s auth has no password method: a user is created without a password and signs in through the directory", s.decl.Auth)
		}
	case passwordOptional:
		if write.password != nil && *write.password == "" {
			return ccc.NilUUID, httpio.NewBadRequestMessage("an empty password is not a password: leave it out for a user who signs in through the directory")
		}
	}
	if wc.snap.byUsername(username) != nil {
		return ccc.NilUUID, httpio.NewConflictMessagef("a user named %q already exists", username)
	}

	var seatScope accesstypes.PolicyScope
	if wc.partition.names.Tenanted {
		if strings.TrimSpace(write.role) == "" {
			return ccc.NilUUID, httpio.NewBadRequestMessage("a role is required: the seat is what makes the user the tenant's")
		}
		scope, ok := wc.snap.roleScope(accesstypes.Role(write.role))
		if !ok {
			return ccc.NilUUID, httpio.NewBadRequestMessagef("role %q does not exist in %s", write.role, wc.partition.scope)
		}
		seatScope = scope
	}

	hasPassword := write.password != nil
	post := map[string]any{
		userAttrUsername:               username,
		userAttrDisabled:               write.disabled,
		userAttrHasPassword:            hasPassword,
		userAttrPasswordChangeRequired: hasPassword,
		userAttrCreatedAt:              time.Now(),
		userAttrLastSignInAt:           nil,
		userAttrOrganization:           nil,
		userAttrRolesSynced:            false,
	}
	fields := make([]accesstypes.Resource, 0, len(write.fields))
	for _, field := range write.fields {
		fields = append(fields, fieldResource(n.Users, wireNameOf(field)))
	}
	if err := wc.judge.checkWrite(ctx, wc.perms, wc.partition.scope, accesstypes.Create, n.Users, fields, nil, post); err != nil {
		return ccc.NilUUID, err
	}
	if wc.partition.names.Tenanted {
		seatPost := map[string]any{seatAttrUser: nil, seatAttrUsername: username, seatAttrRole: write.role, seatAttrEverywhere: false}
		if err := wc.judge.checkWrite(ctx, wc.perms, wc.partition.scope, accesstypes.Create, n.Seats, nil, nil, seatPost); err != nil {
			return ccc.NilUUID, err
		}
	}
	if wc.dryRun {
		return ccc.NilUUID, nil
	}

	id, err := s.accounts.CreateUser(ctx, username, write.password, write.disabled)
	if err != nil {
		return ccc.NilUUID, errors.Wrap(err, "resource.UserAccounts.CreateUser()")
	}
	if wc.partition.names.Tenanted {
		if err := s.seats.AddUserRoles(ctx, seatScope, userKey(username), accesstypes.Role(write.role)); err != nil {
			return ccc.NilUUID, errors.Wrap(err, "resource.UserSeats.AddUserRoles()")
		}
	}

	return id, nil
}

// wireNameOf is a write field's wire name.
func wireNameOf(field accesstypes.Field) string {
	switch field {
	case userFieldUsername:
		return userAttrUsername
	case userFieldPassword:
		return "password"
	case userFieldDisabled:
		return userAttrDisabled
	case userFieldRole:
		return seatAttrRole
	default:
		return strings.ToLower(string(field[:1])) + string(field[1:])
	}
}

// locateUser finds the user a write addresses in the partition: NotFound where there is
// none, or the user is outside the tenant partition.
func (s *UserSurface[A]) locateUser(wc *writeContext, id ccc.UUID) (*composedUser, error) {
	account, ok := wc.snap.byID[id]
	if !ok || !wc.snap.inPartition(account.Username) {
		return nil, httpio.NewNotFoundMessagef("user %s does not exist", id)
	}
	user, err := wc.snap.userRowOf(context.Background(), account)
	if err != nil {
		return nil, err
	}

	return user, nil
}

// seatedOutside reports whether a user of the tenant partition holds seats beyond the
// tenant: in another tenant, in every tenant, or globally. Their account is managed from
// the global partition.
func (s *UserSurface[A]) seatedOutside(ctx context.Context, wc *writeContext, username string) (bool, error) {
	if !wc.partition.names.Tenanted {
		return false, nil
	}
	held, err := s.seats.UserRoles(ctx, userKey(username))
	if err != nil {
		return false, errors.Wrap(err, "resource.UserSeats.UserRoles()")
	}
	own := accesstypes.DomainPolicyScope(wc.partition.domain)
	for scope, roles := range held {
		if scope != own && len(roles) > 0 {
			return true, nil
		}
	}

	return false, nil
}

// accountLevel refuses the account-level actions the tenant partition does not serve
// for a user seated outside the tenant.
func (s *UserSurface[A]) accountLevel(ctx context.Context, wc *writeContext, user *composedUser, action string) error {
	outside, err := s.seatedOutside(ctx, wc, user.account.Username)
	if err != nil {
		return err
	}
	if outside {
		return httpio.NewConflictMessagef("user %q is seated outside this %s, so their account is managed from the global surface; %s is refused here", user.account.Username, strings.ToLower(s.decl.Tenant), action)
	}

	return nil
}

// updateUser changes a user's username or disabled flag. The password is not a field
// here (Set<Auth>UserPassword sets a temporary one), nobody deactivates themselves, and
// a tenant partition manages only its own users' accounts. A renamed user's seats move
// with the name, since the membership store keys them by it.
func (s *UserSurface[A]) updateUser(ctx context.Context, wc *writeContext, id ccc.UUID, write *userWrite) error {
	n := wc.partition.names
	user, err := s.locateUser(wc, id)
	if err != nil {
		return err
	}
	if write.sets(userFieldPassword) {
		return httpio.NewBadRequestMessagef("the password is not updated here: %s sets a temporary one the user changes at their next sign-in", n.SetPassword)
	}
	if write.sets(userFieldRole) {
		return httpio.NewBadRequestMessagef("a role is not updated on the user: seat and unseat through %s", n.Seats)
	}
	if len(write.fields) == 0 {
		return httpio.NewBadRequestMessage("the update sets nothing")
	}
	username := user.account.Username
	if write.sets(userFieldUsername) {
		username = strings.TrimSpace(write.username)
		if username == "" {
			return httpio.NewBadRequestMessage("a username is required")
		}
		if other := wc.snap.byUsername(username); other != nil && other.ID != id {
			return httpio.NewConflictMessagef("a user named %q already exists", username)
		}
	}
	if write.sets(userFieldDisabled) && write.disabled && wc.judge.self(user.account.Username) {
		return httpio.NewForbiddenMessage("you cannot deactivate your own account")
	}
	if err := s.accountLevel(ctx, wc, user, "the update"); err != nil {
		return err
	}

	post := map[string]any{}
	fields := make([]accesstypes.Resource, 0, len(write.fields))
	for _, field := range write.fields {
		fields = append(fields, fieldResource(n.Users, wireNameOf(field)))
		switch field {
		case userFieldUsername:
			post[userAttrUsername] = username
		case userFieldDisabled:
			post[userAttrDisabled] = write.disabled
		}
	}
	if err := wc.judge.checkWrite(ctx, wc.perms, wc.partition.scope, accesstypes.Update, n.Users, fields, user.image(), post); err != nil {
		return err
	}
	if wc.dryRun {
		return nil
	}

	if write.sets(userFieldDisabled) && write.disabled != user.account.Disabled {
		if err := s.accounts.SetUserDisabled(ctx, id, write.disabled); err != nil {
			return errors.Wrap(err, "resource.UserAccounts.SetUserDisabled()")
		}
	}
	if write.sets(userFieldUsername) && username != user.account.Username {
		if err := s.accounts.RenameUser(ctx, id, username); err != nil {
			return errors.Wrap(err, "resource.UserAccounts.RenameUser()")
		}
		if err := s.moveSeats(ctx, user.account.Username, username); err != nil {
			return err
		}
	}

	return nil
}

// moveSeats re-keys a renamed user's memberships: each seat is added under the new name
// and removed under the old, scope by scope. The two stores are not one transaction; a
// failure midway is logged with what moved, and renaming back recovers the rest.
func (s *UserSurface[A]) moveSeats(ctx context.Context, from, to string) error {
	held, err := s.seats.UserRoles(ctx, userKey(from))
	if err != nil {
		return errors.Wrap(err, "resource.UserSeats.UserRoles()")
	}
	for scope, roles := range held {
		if len(roles) == 0 {
			continue
		}
		if err := s.seats.AddUserRoles(ctx, scope, userKey(to), roles...); err != nil {
			logger.FromCtx(ctx).Errorf("user surface: renaming %q to %q: the seats in %s did not move; rename back to recover them: %v", from, to, scope, err)

			return errors.Wrap(err, "resource.UserSeats.AddUserRoles()")
		}
		if err := s.seats.DeleteUserRoles(ctx, scope, userKey(from), roles...); err != nil {
			logger.FromCtx(ctx).Errorf("user surface: renaming %q to %q: the seats in %s are held under both names: %v", from, to, scope, err)

			return errors.Wrap(err, "resource.UserSeats.DeleteUserRoles()")
		}
	}

	return nil
}

// deleteUser removes a user with every seat they hold. Nobody deletes themselves, and a
// tenant partition deletes only its own users.
func (s *UserSurface[A]) deleteUser(ctx context.Context, wc *writeContext, id ccc.UUID) error {
	n := wc.partition.names
	user, err := s.locateUser(wc, id)
	if err != nil {
		return err
	}
	if wc.judge.self(user.account.Username) {
		return httpio.NewForbiddenMessage("you cannot delete your own account")
	}
	if err := s.accountLevel(ctx, wc, user, "the delete"); err != nil {
		return err
	}
	if err := wc.judge.checkWrite(ctx, wc.perms, wc.partition.scope, accesstypes.Delete, n.Users, nil, user.image(), nil); err != nil {
		return err
	}
	if wc.dryRun {
		return nil
	}

	held, err := s.seats.UserRoles(ctx, userKey(user.account.Username))
	if err != nil {
		return errors.Wrap(err, "resource.UserSeats.UserRoles()")
	}
	for scope, roles := range held {
		if len(roles) == 0 {
			continue
		}
		if err := s.seats.DeleteUserRoles(ctx, scope, userKey(user.account.Username), roles...); err != nil {
			return errors.Wrap(err, "resource.UserSeats.DeleteUserRoles()")
		}
	}
	if err := s.accounts.DeleteUser(ctx, id); err != nil {
		return errors.Wrap(err, "resource.UserAccounts.DeleteUser()")
	}

	return nil
}

// seatsPatch is the seats patch handler of a partition: add seats, remove them; a seat
// is never updated.
func (s *UserSurface[A]) seatsPatch(a UserApp, tenanted bool) http.HandlerFunc {
	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := tracer.Start(r.Context())
		defer span.End()

		wc, err := s.writeContext(ctx, r, a, tenanted)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		for op, err := range Operations(r, seatOperationPath, RequireCreatePath()) {
			if err != nil {
				return httpio.NewEncoder(w).ClientMessage(ctx, errors.Wrap(err, "resource.Operations()"))
			}
			user := httpio.Param[ccc.UUID](op.Req, "id1")
			role := accesstypes.Role(httpio.Param[string](op.Req, "id2"))
			switch op.Type {
			case OperationCreate:
				if err := s.createSeat(ctx, wc, user, role); err != nil {
					return httpio.NewEncoder(w).ClientMessage(ctx, err)
				}
			case OperationUpdate:
				return httpio.NewEncoder(w).ClientMessage(ctx, httpio.NewBadRequestMessage("a seat is not updated: unseat and seat"))
			case OperationDelete:
				if err := s.deleteSeat(ctx, wc, user, role); err != nil {
					return httpio.NewEncoder(w).ClientMessage(ctx, err)
				}
			}
		}

		return httpio.NewEncoder(w).Ok(nil)
	})
}

// createSeat seats a user in a role: the role must exist where the partition seats
// (globally or in every tenant for the global partition, in the tenant for the tenant
// partition), the user must be the partition's, and a user whose roles a directory
// writes is not seated by hand.
func (s *UserSurface[A]) createSeat(ctx context.Context, wc *writeContext, id ccc.UUID, role accesstypes.Role) error {
	n := wc.partition.names
	account, ok := wc.snap.byID[id]
	if !ok {
		return httpio.NewBadRequestMessagef("user %s does not exist", id)
	}
	if !wc.snap.inPartition(account.Username) {
		return httpio.NewBadRequestMessagef("user %q is not seated in this %s; seat them from the global surface first", account.Username, strings.ToLower(s.decl.Tenant))
	}
	scope, ok := wc.snap.roleScope(role)
	if !ok {
		return httpio.NewBadRequestMessagef("role %q does not exist in %s", role, wc.partition.scope)
	}
	if wc.snap.seatOf(account, role) != nil {
		return httpio.NewConflictMessagef("user %q already holds %s", account.Username, role)
	}
	facts, err := wc.snap.factsOf(ctx, account)
	if err != nil {
		return err
	}
	if facts.rolesSynced {
		return httpio.NewConflictMessagef("the roles of %q are written by their directory at every sign-in; a seat made here would not last", account.Username)
	}
	seat := &composedSeat{user: account, role: role, scope: scope}
	if err := wc.judge.checkWrite(ctx, wc.perms, wc.partition.scope, accesstypes.Create, n.Seats, nil, nil, seat.image(n.Tenanted)); err != nil {
		return err
	}
	if wc.dryRun {
		return nil
	}
	if err := s.seats.AddUserRoles(ctx, scope, userKey(account.Username), role); err != nil {
		return errors.Wrap(err, "resource.UserSeats.AddUserRoles()")
	}

	return nil
}

// deleteSeat unseats a user from a role. Nobody unseats themselves, a directory's seats
// are not removed by hand, and a seat held everywhere is removed from the global
// partition alone.
func (s *UserSurface[A]) deleteSeat(ctx context.Context, wc *writeContext, id ccc.UUID, role accesstypes.Role) error {
	n := wc.partition.names
	account, ok := wc.snap.byID[id]
	var seat *composedSeat
	if ok {
		seat = wc.snap.seatOf(account, role)
	}
	if seat == nil {
		return httpio.NewNotFoundMessagef("seat %s %s does not exist", id, role)
	}
	if wc.judge.self(account.Username) {
		return httpio.NewForbiddenMessage("you cannot unseat yourself")
	}
	facts, err := wc.snap.factsOf(ctx, account)
	if err != nil {
		return err
	}
	if facts.rolesSynced {
		return httpio.NewConflictMessagef("the roles of %q are written by their directory at every sign-in; unseat them there", account.Username)
	}
	if n.Tenanted && seat.everywhere() {
		return httpio.NewConflictMessagef("%q holds %s in every %s; unseat them from the global surface", account.Username, role, strings.ToLower(s.decl.Tenant))
	}
	if err := wc.judge.checkWrite(ctx, wc.perms, wc.partition.scope, accesstypes.Delete, n.Seats, nil, seat.image(n.Tenanted), nil); err != nil {
		return err
	}
	if wc.dryRun {
		return nil
	}
	if err := s.seats.DeleteUserRoles(ctx, seat.scope, userKey(account.Username), role); err != nil {
		return errors.Wrap(err, "resource.UserSeats.DeleteUserRoles()")
	}

	return nil
}
