package resource

import (
	"net/http"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/tracer"
	"github.com/cccteam/httpio"
)

// DigestOption configures PermissionDigestHandler.
type DigestOption func(*digestOptions)

// digestOptions is what the options set.
type digestOptions struct {
	gates    FeatureGates
	features *FeatureSet
}

// WithFeatureGates filters the digest by the feature flags: an entry for a gated
// resource, one of its fields, or a gated RPC method is left out while the flag is off,
// so the frontend never renders a surface the routes would answer 404 for. The gates
// are the generated declaration (FeatureGates()), the features the application's
// FeatureSet.
func WithFeatureGates(gates FeatureGates, features *FeatureSet) DigestOption {
	return func(o *digestOptions) {
		o.gates = gates
		o.features = features
	}
}

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
// stages. With WithFeatureGates the entries behind a flag that is off are left
// out too.
func PermissionDigestHandler(userPermissions func(r *http.Request) UserPermissions, opts ...DigestOption) http.HandlerFunc {
	var o digestOptions
	for _, opt := range opts {
		opt(&o)
	}

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
		o.gates.filter(o.features, digest)

		return httpio.NewEncoder(w).Ok(digest)
	})
}
