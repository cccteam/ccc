package resource

import (
	"net/http"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/tracer"
	"github.com/cccteam/httpio"
)

// UserDomainsHandler serves the session's tenant list: the sorted list of
// domains where the user holds at least one grant and that the session
// reaches. It is the tenant picker's question, answered by the library so
// applications wire nothing — the generated router registers it on the
// default outlet beside the digest.
//
// The predicate is concealed tenancy's own foothold test over the session's
// admission (UserPermissions.HasGrants, UserPermissions.Reaches), so a domain
// listed here is exactly a domain whose routes answer the user with ordinary
// 403s rather than a concealing 404; the picker and the guard can never
// disagree. A tenant is offered only when the session may enter it, and the
// application's tenant declaration decides (TenantDeclarations): a tenant
// whose declaration does not admit the way the session was established is
// not listed. The global scope is never a domain. The answer reports grants,
// not tenants: a domain the application has since removed still lists while
// grants in it remain, and existence is the tenant roster's
// (TenantRoster.Has). Structural and non-folding, so it caches cleanly per
// user and session. The payload is always a JSON array — an empty membership
// is [], never null.
func UserDomainsHandler(userPermissions func(r *http.Request) UserPermissions) http.HandlerFunc {
	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := tracer.Start(r.Context())
		defer span.End()

		domains, err := userPermissions(r).Domains(ctx)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		if domains == nil {
			domains = []accesstypes.Domain{}
		}

		return httpio.NewEncoder(w).Ok(domains)
	})
}
