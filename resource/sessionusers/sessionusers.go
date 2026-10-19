// Package sessionusers adapts the session library's user API to the resource package's
// account store (resource.UserAccounts), so an application hands its Auth's users to the
// user-management surface in one line: resource.NewUserSurface[router.CrewAuth](
// sessionusers.Accounts(crewAuth.API()), engine.UserManager()).
package sessionusers

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessionstorage"
	"github.com/go-playground/errors/v5"
)

// Accounts adapts an Auth's API: its users, identities and passwords, as the surface
// reads and writes them. A password an operator sets through CreateUser or
// ResetUserPassword carries the forced-change mark.
func Accounts[S, U, Tx any](api *session.AuthAPI[S, U, Tx]) resource.UserAccounts {
	return &accounts[S, U, Tx]{api: api}
}

type accounts[S, U, Tx any] struct {
	api *session.AuthAPI[S, U, Tx]
}

func (a *accounts[S, U, Tx]) Users(ctx context.Context) ([]resource.UserAccount, error) {
	users, err := a.api.Users(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "session.AuthAPI.Users()")
	}
	accounts := make([]resource.UserAccount, 0, len(users))
	for _, user := range users {
		accounts = append(accounts, accountOf(user))
	}

	return accounts, nil
}

func (a *accounts[S, U, Tx]) User(ctx context.Context, id ccc.UUID) (*resource.UserAccount, error) {
	user, err := a.api.User(ctx, id)
	if err != nil {
		return nil, errors.Wrap(err, "session.AuthAPI.User()")
	}
	account := accountOf(user)

	return &account, nil
}

func (a *accounts[S, U, Tx]) CreateUser(ctx context.Context, username string, password *string, disabled bool) (ccc.UUID, error) {
	id, err := a.api.CreateSessionUser(ctx, &session.CreateUserRequest{
		Username:               username,
		Password:               password,
		Disabled:               disabled,
		PasswordChangeRequired: password != nil,
	})
	if err != nil {
		return ccc.NilUUID, errors.Wrap(err, "session.AuthAPI.CreateSessionUser()")
	}

	return id, nil
}

func (a *accounts[S, U, Tx]) RenameUser(ctx context.Context, id ccc.UUID, username string) error {
	if err := a.api.ChangeSessionUserUsername(ctx, id, username); err != nil {
		return errors.Wrap(err, "session.AuthAPI.ChangeSessionUserUsername()")
	}

	return nil
}

func (a *accounts[S, U, Tx]) SetUserDisabled(ctx context.Context, id ccc.UUID, disabled bool) error {
	if disabled {
		if err := a.api.DeactivateSessionUser(ctx, id); err != nil {
			return errors.Wrap(err, "session.AuthAPI.DeactivateSessionUser()")
		}

		return nil
	}
	if err := a.api.ActivateSessionUser(ctx, id); err != nil {
		return errors.Wrap(err, "session.AuthAPI.ActivateSessionUser()")
	}

	return nil
}

func (a *accounts[S, U, Tx]) DeleteUser(ctx context.Context, id ccc.UUID) error {
	if err := a.api.DeleteSessionUser(ctx, id); err != nil {
		return errors.Wrap(err, "session.AuthAPI.DeleteSessionUser()")
	}

	return nil
}

func (a *accounts[S, U, Tx]) ResetUserPassword(ctx context.Context, id ccc.UUID, temporary string) error {
	if err := a.api.ResetSessionUserPassword(ctx, id, temporary); err != nil {
		return errors.Wrap(err, "session.AuthAPI.ResetSessionUserPassword()")
	}

	return nil
}

func (a *accounts[S, U, Tx]) Identities(ctx context.Context, id ccc.UUID) ([]resource.UserIdentity, error) {
	identities, err := a.api.Identities(ctx, id)
	if err != nil {
		return nil, errors.Wrap(err, "session.AuthAPI.Identities()")
	}
	linked := make([]resource.UserIdentity, 0, len(identities))
	for _, identity := range identities {
		linked = append(linked, resource.UserIdentity{
			ID:           identity.ID,
			Method:       string(identity.Method),
			Connection:   identity.Connection,
			Subject:      identity.Subject,
			Organization: identity.Tenant,
			Email:        identity.EmailAtLink,
			LinkedAt:     identity.CreatedAt,
			LastUsedAt:   identity.LastUsedAt,
		})
	}

	return linked, nil
}

func (a *accounts[S, U, Tx]) UnlinkIdentity(ctx context.Context, identityID ccc.UUID) error {
	if err := a.api.UnlinkIdentity(ctx, identityID); err != nil {
		return errors.Wrap(err, "session.AuthAPI.UnlinkIdentity()")
	}

	return nil
}

// accountOf maps a stored user to an account.
func accountOf(user *sessionstorage.SessionUser) resource.UserAccount {
	return resource.UserAccount{
		ID:                     user.ID,
		Username:               user.Username,
		HasPassword:            user.PasswordHash != nil,
		PasswordChangeRequired: user.PasswordChangeRequired,
		Disabled:               user.Disabled,
		CreatedAt:              user.CreatedAt,
		LastSignInAt:           user.LastSignInAt,
	}
}
