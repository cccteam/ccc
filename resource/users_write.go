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
	"github.com/cccteam/session/sessioninfo"
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
	IDs []ccc.UUID `json:"ids"`
}

// The operation path patterns: a user is addressed by its id, a seat by its user and
// role.
const (
	userOperationPath = "/{id}"
	seatOperationPath = "/{id1}/{id2}"
)

// writeContext is what one write knows: the request's partition and caller, the
// snapshot it judges against, composed after the grants are checked and never for a
// caller without them, and whether it applies anything.
type writeContext struct {
	partition *userPartition
	perms     UserPermissions
	dryRun    bool
	load      func(ctx context.Context) (*userSnapshot, error)
	snap      *userSnapshot
	judge     *userJudge
}

// ensure composes the snapshot once.
func (wc *writeContext) ensure(ctx context.Context) error {
	if wc.snap != nil {
		return nil
	}
	snap, err := wc.load(ctx)
	if err != nil {
		return err
	}
	wc.snap, wc.judge = snap, newUserJudge(snap, wc.perms)

	return nil
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

		wc := s.writeContext(r, a, tenanted)
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
				if err := s.updateUser(ctx, wc, op, write); err != nil {
					return httpio.NewEncoder(w).ClientMessage(ctx, err)
				}
			case OperationDelete:
				if err := s.deleteUser(ctx, wc, op); err != nil {
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
func (s *UserSurface[A]) writeContext(r *http.Request, a UserApp, tenanted bool) *writeContext {
	partition := s.partitionOf(r, tenanted)

	return &writeContext{
		partition: partition,
		perms:     a.UserPermissions(r),
		dryRun:    IsDryRun(r),
		load:      func(ctx context.Context) (*userSnapshot, error) { return s.snapshot(ctx, partition) },
	}
}

// userWrite is one decoded user operation: the fields it sets and their values.
type userWrite struct {
	fields   []accesstypes.Field
	username string
	password *string
	email    *string
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
	if write.sets(userFieldEmail) {
		write.email, _ = patchSet.Get(userFieldEmail).(*string)
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
	fields := make([]accesstypes.Resource, 0, len(write.fields))
	for _, field := range write.fields {
		fields = append(fields, fieldResource(n.Users, wireNameOf(field)))
	}
	decisions, err := checkWriteGrants(ctx, wc.perms, wc.partition.scope, accesstypes.Create, n.Users, fields)
	if err != nil {
		return ccc.NilUUID, err
	}
	var seatDecisions accesstypes.Decisions
	if n.Tenanted {
		if seatDecisions, err = checkWriteGrants(ctx, wc.perms, wc.partition.scope, accesstypes.Create, n.Seats, nil); err != nil {
			return ccc.NilUUID, err
		}
	}
	if err := wc.ensure(ctx); err != nil {
		return ccc.NilUUID, err
	}
	username, err := s.validateCreate(wc, write)
	if err != nil {
		return ccc.NilUUID, err
	}
	email, err := s.validateEmail(write)
	if err != nil {
		return ccc.NilUUID, err
	}
	var seatScope accesstypes.PolicyScope
	if n.Tenanted {
		if seatScope, err = s.validateTenantRole(wc, write); err != nil {
			return ccc.NilUUID, err
		}
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
		userAttrEmail:                  nil,
		userAttrEmailVerified:          false,
		userAttrRolesSynced:            false,
	}
	if email != nil {
		post[userAttrEmail] = *email
	}
	if err := wc.judge.judgeWrite(ctx, decisions, wc.partition.scope, accesstypes.Create, nil, post); err != nil {
		return ccc.NilUUID, err
	}
	if n.Tenanted {
		seatPost := map[string]any{seatAttrUser: nil, seatAttrUsername: username, seatAttrRole: write.role, seatAttrEverywhere: false}
		if err := wc.judge.judgeWrite(ctx, seatDecisions, wc.partition.scope, accesstypes.Create, nil, seatPost); err != nil {
			return ccc.NilUUID, err
		}
	}
	if wc.dryRun {
		return ccc.NilUUID, nil
	}

	id, err := s.accounts.CreateUser(ctx, username, write.password, email, write.disabled)
	if err != nil {
		return ccc.NilUUID, errors.Wrap(err, "resource.UserAccounts.CreateUser()")
	}
	if n.Tenanted {
		if err := s.seats.AddUserRoles(ctx, seatScope, userKey(username), accesstypes.Role(write.role)); err != nil {
			return ccc.NilUUID, errors.Wrap(err, "resource.UserSeats.AddUserRoles()")
		}
	}

	return id, nil
}

// validateCreate applies the shape to a create: whether users are created here, the
// username, the password rule, and a name already in use. It returns the username as
// the account takes it.
func (s *UserSurface[A]) validateCreate(wc *writeContext, write *userWrite) (string, error) {
	if !s.decl.Shape.CreatesUsers() {
		return "", httpio.NewBadRequestMessagef("the %s auth creates no users: its people are created by their first sign-in through the directory", s.decl.Auth)
	}
	username := strings.TrimSpace(write.username)
	if username == "" {
		return "", httpio.NewBadRequestMessage("a username is required")
	}
	switch s.decl.Shape.passwordOnCreate() {
	case passwordRequired:
		if write.password == nil || *write.password == "" {
			return "", httpio.NewBadRequestMessage("a password is required: the password is the way in")
		}
	case passwordRefused:
		if write.password != nil {
			return "", httpio.NewBadRequestMessagef("the %s auth has no password method: a user is created without a password and signs in through the directory", s.decl.Auth)
		}
	case passwordOptional:
		if write.password != nil && *write.password == "" {
			return "", httpio.NewBadRequestMessage("an empty password is not a password: leave it out for a user who signs in through the directory")
		}
	}
	if wc.snap.byUsername(username) != nil {
		return "", httpio.NewConflictMessagef("a user named %q already exists", username)
	}

	return username, nil
}

// validateEmail reads the address a create or update enters, an operator's entry the
// person verifies later: none where the write sets none, refused where the username is
// the address (the store derives it), and never empty.
func (s *UserSurface[A]) validateEmail(write *userWrite) (*string, error) {
	if !write.sets(userFieldEmail) {
		return nil, nil
	}
	if s.decl.Shape.UsernameIsEmail {
		return nil, httpio.NewBadRequestMessage("the username is the person's email address here: give the address as the username, and leave email out")
	}
	if write.email == nil {
		return nil, nil
	}
	email := strings.TrimSpace(*write.email)
	if email == "" {
		return nil, httpio.NewBadRequestMessage("an empty address is not an address: leave email out, or null to remove it")
	}

	return &email, nil
}

// validateTenantRole reads the seat a tenant create gives the new user: a role that
// exists in the tenant.
func (s *UserSurface[A]) validateTenantRole(wc *writeContext, write *userWrite) (accesstypes.PolicyScope, error) {
	if strings.TrimSpace(write.role) == "" {
		return accesstypes.PolicyScope{}, httpio.NewBadRequestMessage("a role is required: the seat is what makes the user the tenant's")
	}
	scope, ok := wc.snap.roleScope(accesstypes.Role(write.role))
	if !ok {
		return accesstypes.PolicyScope{}, httpio.NewBadRequestMessagef("role %q does not exist in %s", write.role, wc.partition.scope)
	}

	return scope, nil
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
	case userFieldEmail:
		return userAttrEmail
	case userFieldRole:
		return seatAttrRole
	default:
		return strings.ToLower(string(field[:1])) + string(field[1:])
	}
}

// operationUUID reads a UUID the operation's path addresses; a malformed one is a bad
// request, never a panic.
func operationUUID(op *Operation, param httpio.ParamType) (ccc.UUID, error) {
	raw := httpio.Param[string](op.Req, param)
	id, err := ccc.UUIDFromString(raw)
	if err != nil {
		return ccc.NilUUID, httpio.NewBadRequestMessagef("%q is not an id", raw)
	}

	return id, nil
}

// locateUser finds the user a write addresses in the partition: NotFound where there is
// none, or the user is outside the tenant partition.
func (s *UserSurface[A]) locateUser(ctx context.Context, wc *writeContext, id ccc.UUID) (*composedUser, error) {
	account, ok := wc.snap.byID[id]
	if !ok || !wc.snap.inPartition(account.Username) {
		return nil, httpio.NewNotFoundMessagef("user %s does not exist", id)
	}
	user, err := wc.snap.userRowOf(ctx, account)
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
func (s *UserSurface[A]) updateUser(ctx context.Context, wc *writeContext, op *Operation, write *userWrite) error {
	n := wc.partition.names
	if err := refuseUpdateShape(&n, write); err != nil {
		return err
	}
	fields := make([]accesstypes.Resource, 0, len(write.fields))
	for _, field := range write.fields {
		fields = append(fields, fieldResource(n.Users, wireNameOf(field)))
	}
	decisions, err := checkWriteGrants(ctx, wc.perms, wc.partition.scope, accesstypes.Update, n.Users, fields)
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
	user, err := s.locateUser(ctx, wc, id)
	if err != nil {
		return err
	}
	username := user.account.Username
	if write.sets(userFieldUsername) {
		username = strings.TrimSpace(write.username)
		if username == "" {
			return httpio.NewBadRequestMessage("a username is required")
		}
		if s.decl.Shape.UsernameIsEmail && username != user.account.Username {
			return httpio.NewConflictMessage("the username is the person's email address: it changes when the person verifies a new address, not by a rename")
		}
		if other := wc.snap.byUsername(username); other != nil && other.ID != id {
			return httpio.NewConflictMessagef("a user named %q already exists", username)
		}
	}
	email, err := s.validateEmail(write)
	if err != nil {
		return err
	}
	if write.sets(userFieldDisabled) && write.disabled && wc.judge.self(user.account.Username) {
		return httpio.NewForbiddenMessage("you cannot deactivate your own account")
	}
	if err := s.accountLevel(ctx, wc, user, "the update"); err != nil {
		return err
	}

	post := map[string]any{}
	if write.sets(userFieldUsername) {
		post[userAttrUsername] = username
	}
	if write.sets(userFieldDisabled) {
		post[userAttrDisabled] = write.disabled
	}
	if write.sets(userFieldEmail) {
		post[userAttrEmail] = nil
		if email != nil {
			post[userAttrEmail] = *email
		}
	}
	if err := wc.judge.judgeWrite(ctx, decisions, wc.partition.scope, accesstypes.Update, user.image(), post); err != nil {
		return err
	}
	if wc.dryRun {
		return nil
	}

	return s.applyUserUpdate(ctx, user, write, username, email)
}

// refuseUpdateShape refuses what an update never sets: the password, a role, or
// nothing at all.
func refuseUpdateShape(n *UserPartitionNames, write *userWrite) error {
	switch {
	case write.sets(userFieldPassword):
		return httpio.NewBadRequestMessagef("the password is not updated here: %s sets a temporary one the user changes at their next sign-in", n.SetPassword)
	case write.sets(userFieldRole):
		return httpio.NewBadRequestMessagef("a role is not updated on the user: seat and unseat through %s", n.Seats)
	case len(write.fields) == 0:
		return httpio.NewBadRequestMessage("the update sets nothing")
	default:
		return nil
	}
}

// applyUserUpdate writes a checked update to the stores: the flag, the address, then
// the rename with its seats.
func (s *UserSurface[A]) applyUserUpdate(ctx context.Context, user *composedUser, write *userWrite, username string, email *string) error {
	if write.sets(userFieldDisabled) && write.disabled != user.account.Disabled {
		if err := s.accounts.SetUserDisabled(ctx, user.account.ID, write.disabled); err != nil {
			return errors.Wrap(err, "resource.UserAccounts.SetUserDisabled()")
		}
	}
	if write.sets(userFieldEmail) {
		if err := s.accounts.SetUserEmail(ctx, user.account.ID, email); err != nil {
			return errors.Wrap(err, "resource.UserAccounts.SetUserEmail()")
		}
	}
	if write.sets(userFieldUsername) && username != user.account.Username {
		if err := s.accounts.RenameUser(ctx, user.account.ID, username); err != nil {
			return errors.Wrap(err, "resource.UserAccounts.RenameUser()")
		}
		if err := moveSeats(ctx, s.seats, user.account.Username, username); err != nil {
			return err
		}
	}

	return nil
}

// moveSeats re-keys a renamed user's memberships: each seat is added under the new name
// and removed under the old, scope by scope. The two stores are not one transaction; a
// failure midway is logged with what moved, and running it again finishes the move
// (adding a seat that exists is a no-op).
func moveSeats(ctx context.Context, seats UserSeats, from, to string) error {
	held, err := seats.UserRoles(ctx, userKey(from))
	if err != nil {
		return errors.Wrap(err, "resource.UserSeats.UserRoles()")
	}
	for scope, roles := range held {
		if len(roles) == 0 {
			continue
		}
		if err := seats.AddUserRoles(ctx, scope, userKey(to), roles...); err != nil {
			logger.FromCtx(ctx).Errorf("user surface: renaming %q to %q: the seats in %s did not move; run the move again to recover them: %v", from, to, scope, err)

			return errors.Wrap(err, "resource.UserSeats.AddUserRoles()")
		}
		if err := seats.DeleteUserRoles(ctx, scope, userKey(from), roles...); err != nil {
			logger.FromCtx(ctx).Errorf("user surface: renaming %q to %q: the seats in %s are held under both names: %v", from, to, scope, err)

			return errors.Wrap(err, "resource.UserSeats.DeleteUserRoles()")
		}
	}

	return nil
}

// SeatsFollowRename is the session library's rename hook (session.Auth.SetUserRenamed)
// over the Auth's membership store: where the username is the person's email address, a
// verified new address renames the user, and the seats held under the old name move to
// the new one, as the surface's own rename moves them. Safe to run again after a
// failure midway.
func SeatsFollowRename(seats UserSeats) func(ctx context.Context, userID ccc.UUID, from, to string) error {
	return func(ctx context.Context, _ ccc.UUID, from, to string) error {
		return moveSeats(ctx, seats, from, to)
	}
}

// deleteOrphan applies the orphan rule once a seat is removed: a user who then holds no
// seat anywhere, in any tenant, in every tenant or in the global partition, is deleted
// with their identity links and sessions, since an account nothing admits causes
// trouble kept; under a directory's open rule the next sign-in creates the row again,
// and under password sign-up the person signs up again. A user with a seat somewhere,
// a global role above all, stays.
func (s *UserSurface[A]) deleteOrphan(ctx context.Context, account *UserAccount) error {
	held, err := s.seats.UserRoles(ctx, userKey(account.Username))
	if err != nil {
		return errors.Wrap(err, "resource.UserSeats.UserRoles()")
	}
	for _, roles := range held {
		if len(roles) > 0 {
			return nil
		}
	}
	if err := s.accounts.DeleteUser(ctx, account.ID); err != nil {
		return errors.Wrap(err, "resource.UserAccounts.DeleteUser()")
	}
	logger.FromCtx(ctx).Infof("user surface: %q holds no seat anywhere after the removal, so the account is deleted with its identity links (the orphan rule)", account.Username)

	return nil
}

// deleteUser removes a user. In the global partition the account goes with every seat
// they hold. In a tenant partition the delete is the tenant's: the user's seats in the
// tenant are removed, which ends their membership there, and the account goes only when
// no seat remains anywhere (the orphan rule); a user who holds a role in every tenant is
// managed from the global surface. Nobody deletes themselves.
func (s *UserSurface[A]) deleteUser(ctx context.Context, wc *writeContext, op *Operation) error {
	n := wc.partition.names
	decisions, err := checkWriteGrants(ctx, wc.perms, wc.partition.scope, accesstypes.Delete, n.Users, nil)
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
	user, err := s.locateUser(ctx, wc, id)
	if err != nil {
		return err
	}
	if wc.judge.self(user.account.Username) {
		return httpio.NewForbiddenMessage("you cannot delete your own account")
	}
	if n.Tenanted {
		return s.removeFromTenant(ctx, wc, decisions, user)
	}
	if err := wc.judge.judgeWrite(ctx, decisions, wc.partition.scope, accesstypes.Delete, user.image(), nil); err != nil {
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

// removeFromTenant is the tenant partition's delete: the user's seats in the tenant go,
// and the account with them when no seat remains anywhere.
func (s *UserSurface[A]) removeFromTenant(ctx context.Context, wc *writeContext, decisions accesstypes.Decisions, user *composedUser) error {
	if len(user.everywhereRoles) > 0 {
		return httpio.NewConflictMessagef("user %q holds %s in every %s; their account is managed from the global surface", user.account.Username, strings.Join(user.everywhereRoles, ", "), strings.ToLower(s.decl.Tenant))
	}
	if err := wc.judge.judgeWrite(ctx, decisions, wc.partition.scope, accesstypes.Delete, user.image(), nil); err != nil {
		return err
	}
	if wc.dryRun {
		return nil
	}
	scope := accesstypes.DomainPolicyScope(wc.partition.domain)
	held := wc.snap.seats[userKey(user.account.Username)][scope]
	if len(held) > 0 {
		if err := s.seats.DeleteUserRoles(ctx, scope, userKey(user.account.Username), held...); err != nil {
			return errors.Wrap(err, "resource.UserSeats.DeleteUserRoles()")
		}
	}

	return s.deleteOrphan(ctx, user.account)
}

// seatsPatch is the seats patch handler of a partition: add seats, remove them; a seat
// is never updated.
func (s *UserSurface[A]) seatsPatch(a UserApp, tenanted bool) http.HandlerFunc {
	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := tracer.Start(r.Context())
		defer span.End()

		wc := s.writeContext(r, a, tenanted)
		for op, err := range Operations(r, seatOperationPath, RequireCreatePath()) {
			if err != nil {
				return httpio.NewEncoder(w).ClientMessage(ctx, errors.Wrap(err, "resource.Operations()"))
			}
			switch op.Type {
			case OperationCreate:
				if err := s.createSeat(ctx, wc, op); err != nil {
					return httpio.NewEncoder(w).ClientMessage(ctx, err)
				}
			case OperationUpdate:
				return httpio.NewEncoder(w).ClientMessage(ctx, httpio.NewBadRequestMessage("a seat is not updated: unseat and seat"))
			case OperationDelete:
				if err := s.deleteSeat(ctx, wc, op); err != nil {
					return httpio.NewEncoder(w).ClientMessage(ctx, err)
				}
			}
		}

		return httpio.NewEncoder(w).Ok(nil)
	})
}

// createSeat seats a user in a role: the role must exist where the partition seats
// (globally or in every tenant for the global partition, in the tenant for the tenant
// partition), the user must be the partition's or vouched for by the tenant's own
// directory (vouchedFor), and a user whose roles a directory writes is not seated by
// hand.
func (s *UserSurface[A]) createSeat(ctx context.Context, wc *writeContext, op *Operation) error {
	n := wc.partition.names
	decisions, err := checkWriteGrants(ctx, wc.perms, wc.partition.scope, accesstypes.Create, n.Seats, nil)
	if err != nil {
		return err
	}
	if err := wc.ensure(ctx); err != nil {
		return err
	}
	id, err := operationUUID(op, "id1")
	if err != nil {
		return err
	}
	role := accesstypes.Role(httpio.Param[string](op.Req, "id2"))
	account, ok := wc.snap.byID[id]
	if !ok {
		return httpio.NewBadRequestMessagef("user %s does not exist", id)
	}
	if !wc.snap.inPartition(account.Username) {
		if err := s.vouchedFor(ctx, wc, account); err != nil {
			return err
		}
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
	if err := wc.judge.judgeWrite(ctx, decisions, wc.partition.scope, accesstypes.Create, nil, seat.image(n.Tenanted)); err != nil {
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

// vouchedFor is the second door into a tenant: a user who holds no seat in the tenant
// may be seated there when they signed in through a directory the tenant declares
// (their identity's method and connection are among the tenant's directories): the
// person entered through the tenant's own directory, was told they have no access yet,
// and asked their administrator. Anyone else enters by invitation, or is seated from
// the global surface.
func (s *UserSurface[A]) vouchedFor(ctx context.Context, wc *writeContext, account *UserAccount) error {
	tenant := strings.ToLower(s.decl.Tenant)
	if s.settings.declarations == nil {
		return httpio.NewBadRequestMessagef("user %q is not seated in this %s: invite them, or seat them from the global surface", account.Username, tenant)
	}
	declaration, err := s.settings.declarations(ctx, wc.partition.domain)
	if err != nil {
		return errors.Wrap(err, "resource.TenantDeclarations()")
	}
	facts, err := wc.snap.factsOf(ctx, account)
	if err != nil {
		return err
	}
	for i := range facts.identities {
		identity := &facts.identities[i]
		if declaration != nil && declaration.Admits(sessioninfo.AuthMethod(identity.Method), identity.Connection) {
			return nil
		}
	}

	return httpio.NewBadRequestMessagef("user %q is not seated in this %s and entered through no directory it declares: invite them, or seat them from the global surface", account.Username, tenant)
}

// deleteSeat unseats a user from a role. Nobody unseats themselves, a directory's seats
// are not removed by hand, a seat held everywhere is removed from the global partition
// alone, and a user left with no seat anywhere is deleted (the orphan rule).
func (s *UserSurface[A]) deleteSeat(ctx context.Context, wc *writeContext, op *Operation) error {
	n := wc.partition.names
	decisions, err := checkWriteGrants(ctx, wc.perms, wc.partition.scope, accesstypes.Delete, n.Seats, nil)
	if err != nil {
		return err
	}
	if err := wc.ensure(ctx); err != nil {
		return err
	}
	id, err := operationUUID(op, "id1")
	if err != nil {
		return err
	}
	role := accesstypes.Role(httpio.Param[string](op.Req, "id2"))
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
	if err := wc.judge.judgeWrite(ctx, decisions, wc.partition.scope, accesstypes.Delete, seat.image(n.Tenanted), nil); err != nil {
		return err
	}
	if wc.dryRun {
		return nil
	}
	if err := s.seats.DeleteUserRoles(ctx, seat.scope, userKey(account.Username), role); err != nil {
		return errors.Wrap(err, "resource.UserSeats.DeleteUserRoles()")
	}

	return s.deleteOrphan(ctx, account)
}
