package resource

import (
	"context"
	"net/http"
	"strings"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/tracer"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

// The user surface's methods: the operator's actions on one account, Execute on the
// method, its grant's condition judged over the target user's row.

// SetUserPasswordRequest is the body of Set<Auth>UserPassword: the user and the temporary
// password an operator chose, which the user changes at their next sign-in.
type SetUserPasswordRequest struct {
	User     ccc.UUID `json:"user"`
	Password string   `json:"password"`
}

// UnlinkUserIdentityRequest is the body of Unlink<Auth>UserIdentity: the user and the
// identity to unlink from them.
type UnlinkUserIdentityRequest struct {
	User     ccc.UUID `json:"user"`
	Identity ccc.UUID `json:"identity"`
}

// methodContext decodes a method's body and checks Execute on it, then locates the
// target user in the partition and judges the grant's condition over their row.
func (s *UserSurface[A]) methodTarget(ctx context.Context, r *http.Request, a UserApp, tenanted bool, method accesstypes.Resource, user ccc.UUID) (*writeContext, *composedUser, error) {
	wc := s.writeContext(r, a, tenanted)
	decisions, err := wc.perms.Check(ctx, RequestEnvironment(), wc.partition.scope, accesstypes.Execute, method)
	if err != nil {
		return nil, nil, errors.Wrap(err, "resource.UserPermissions.Check()")
	}
	if denied := decisions.DeniedResources(); len(denied) > 0 {
		return nil, nil, httpio.NewForbiddenMessagef("user %s, scope %s, does not have %s on %s", wc.perms.User(), wc.partition.scope, accesstypes.Execute, denied)
	}
	if err := wc.ensure(ctx); err != nil {
		return nil, nil, err
	}
	target, err := s.locateUser(ctx, wc, user)
	if err != nil {
		return nil, nil, err
	}
	ok, err := wc.judge.permits(ctx, decisions[method], target.image(), nil)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, httpio.NewForbiddenMessagef("%s may not run against user %s", method, user)
	}

	return wc, target, nil
}

// setUserPassword is Set<Auth>UserPassword: a temporary password on the account, which
// takes the forced-change mark and loses its sessions. One's own password is changed
// through the password change, not reset here.
func (s *UserSurface[A]) setUserPassword(a UserApp, tenanted bool) http.HandlerFunc {
	decoder, err := newStructDecoder[SetUserPasswordRequest]()
	if err != nil {
		panic(err)
	}
	decoder = decoder.WithValidator(a.Validator())

	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := tracer.Start(r.Context())
		defer span.End()

		req, err := decoder.Decode(r)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		method := s.partitionOf(r, tenanted).names.SetPassword
		wc, target, err := s.methodTarget(ctx, r, a, tenanted, method, req.User)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		if strings.TrimSpace(req.Password) == "" {
			return httpio.NewEncoder(w).ClientMessage(ctx, httpio.NewBadRequestMessage("a password is required"))
		}
		if wc.judge.self(target.account.Username) {
			return httpio.NewEncoder(w).ClientMessage(ctx, httpio.NewBadRequestMessage("change your own password through the password change, which asks for the current one"))
		}
		if err := s.accountLevel(ctx, wc, target, "the password reset"); err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		if wc.dryRun {
			return httpio.NewEncoder(w).Ok(nil)
		}
		if err := s.accounts.ResetUserPassword(ctx, target.account.ID, req.Password); err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, errors.Wrap(err, "resource.UserAccounts.ResetUserPassword()"))
		}

		return httpio.NewEncoder(w).Ok(nil)
	})
}

// unlinkUserIdentity is Unlink<Auth>UserIdentity: an identity taken off the account. The
// account's only way in is never unlinked.
func (s *UserSurface[A]) unlinkUserIdentity(a UserApp, tenanted bool) http.HandlerFunc {
	decoder, err := newStructDecoder[UnlinkUserIdentityRequest]()
	if err != nil {
		panic(err)
	}
	decoder = decoder.WithValidator(a.Validator())

	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := tracer.Start(r.Context())
		defer span.End()

		req, err := decoder.Decode(r)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		method := s.partitionOf(r, tenanted).names.UnlinkIdentity
		wc, target, err := s.methodTarget(ctx, r, a, tenanted, method, req.User)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		var linked *UserIdentity
		for i := range target.facts.identities {
			if target.facts.identities[i].ID == req.Identity {
				linked = &target.facts.identities[i]
			}
		}
		if linked == nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, httpio.NewNotFoundMessagef("identity %s is not linked to user %s", req.Identity, req.User))
		}
		if !target.account.HasPassword && len(target.facts.identities) == 1 {
			return httpio.NewEncoder(w).ClientMessage(ctx, httpio.NewConflictMessagef("the identity is the only way %q signs in; set a password or delete the account instead", target.account.Username))
		}
		if err := s.accountLevel(ctx, wc, target, "the unlinking"); err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		if wc.dryRun {
			return httpio.NewEncoder(w).Ok(nil)
		}
		if err := s.accounts.UnlinkIdentity(ctx, linked.ID); err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, errors.Wrap(err, "resource.UserAccounts.UnlinkIdentity()"))
		}

		return httpio.NewEncoder(w).Ok(nil)
	})
}
