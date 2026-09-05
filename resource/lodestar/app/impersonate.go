package app

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/cccteam/httpio"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessioninfo"
	"github.com/go-playground/errors/v5"
)

// impersonateRequest is the mint route's body: view as a user (read-only by
// default) or act as a role.
type impersonateRequest struct {
	// Kind is "user" or "role".
	Kind string `json:"kind"`
	// Principal is the user or role name.
	Principal string `json:"principal"`
	// Mask lists the permissions the session keeps; empty means the kind's default —
	// List and Read for a user, unrestricted for a role.
	Mask []accesstypes.Permission `json:"mask"`
	// Reason is free text recorded on the impersonation record.
	Reason string `json:"reason"`
}

// impersonateResponse names the session that was minted.
type impersonateResponse struct {
	SessionID ccc.UUID `json:"sessionId"`
}

// Impersonate mints an impersonated session on behalf of the authenticated actor: a
// "view as" session that operates as another user under a List, Read mask, or an
// "act as" session that operates as a role. Authorization stays with the app — the
// two manual Execute registrations (resources.ViewAsUser, resources.AssumeRole),
// checked in the global scope and held by the Governor and the Marshal only; the
// library refuses chaining (an impersonated session cannot mint another) and writes
// the record atomically with the session. The response cookie replaces the actor's
// session; the actor's own session is linked as the source.
func (a *App) Impersonate() http.HandlerFunc {
	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()

		var req impersonateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, httpio.NewBadRequestMessagef("decoding request: %s", err))
		}
		if req.Principal == "" {
			return httpio.NewEncoder(w).ClientMessage(ctx, httpio.NewBadRequestMessage("principal is required"))
		}

		// The zero mask is the unrestricted session; a mask built from no permissions
		// would allow nothing, so it is only ever built from an explicit list — or the
		// view-as default, List and Read.
		var gate accesstypes.Resource
		var principal accesstypes.Principal
		var mask accesstypes.PermissionMask
		if len(req.Mask) > 0 {
			mask = accesstypes.MaskPermissions(req.Mask...)
		}
		switch req.Kind {
		case "user":
			gate = resources.ViewAsUser
			principal = accesstypes.UserPrincipal(accesstypes.User(req.Principal))
			if len(req.Mask) == 0 {
				mask = accesstypes.MaskPermissions(accesstypes.List, accesstypes.Read)
			}
		case "role":
			gate = resources.AssumeRole
			principal = accesstypes.RolePrincipal(accesstypes.Role(req.Principal))
		default:
			return httpio.NewEncoder(w).ClientMessage(ctx, httpio.NewBadRequestMessagef("kind %q must be user or role", req.Kind))
		}

		perms := a.UserPermissions(r)
		env := accesstypes.NewEnvironment().WithNow(time.Now())
		decisions, err := perms.Check(ctx, env, accesstypes.GlobalScope(), accesstypes.Execute, gate)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, errors.Wrap(err, "resource.UserPermissions.Check()"))
		}
		if !decisions[gate].IsGranted() {
			return httpio.NewEncoder(w).ClientMessage(ctx, httpio.NewForbiddenMessagef("user %s does not have Execute on %s", perms.User(), gate))
		}

		info := sessioninfo.FromCtx(ctx)
		id, err := a.API().StartImpersonatedSession(ctx, w, &session.ImpersonationRequest{
			Actor:           info.Username,
			SourceSessionID: ccc.NullUUID{UUID: info.ID, Valid: !info.ID.IsNil()},
			Principal:       principal,
			Mask:            mask,
			Reason:          req.Reason,
		})
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, errors.Wrap(err, "session.PasswordAuthAPI.StartImpersonatedSession()"))
		}

		return httpio.NewEncoder(w).Ok(impersonateResponse{SessionID: id})
	})
}
