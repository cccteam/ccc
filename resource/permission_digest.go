package resource

import (
	"net/http"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/tracer"
	"github.com/cccteam/httpio"
)

// PermissionDigestHandler serves the per-scope permission digest: the
// session user's structural grant enumeration, resource → permission →
// granted|conditional, with denied targets absent so consumers fail closed.
// The generated router registers it on the default outlet; applications wire
// nothing.
//
// The scope is the request's input, never payload structure: ?domain= names
// one tenant partition, its absence means the global scope. There is no
// domain validation — an unknown tenant simply holds no grants, so its
// digest is empty, which also keeps concealed tenancy unprobeable from this
// endpoint. The payload is advisory UI material (which surfaces to render);
// enforcement stays with the endpoint gate, the read rules, and the write
// stages.
func PermissionDigestHandler(userPermissions func(r *http.Request) UserPermissions) http.HandlerFunc {
	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := tracer.Start(r.Context())
		defer span.End()

		scope := accesstypes.GlobalScope()
		if domain := r.URL.Query().Get("domain"); domain != "" {
			scope = accesstypes.DomainScope(accesstypes.Domain(domain))
		}

		digest, err := userPermissions(r).PermissionDigest(ctx, scope)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}

		return httpio.NewEncoder(w).Ok(digest)
	})
}
