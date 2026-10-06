package accesstypes

// DigestState is one PermissionDigest entry's answer: granted when at least
// one unconditional grant covers the target, conditional when only
// conditional grants do. There is no denied state — a denied target is
// absent from the digest entirely, so a consumer that misses a key fails
// closed.
type DigestState string

// The digest states. There is deliberately no denied constant: denial is
// expressed by absence.
const (
	DigestGranted     DigestState = "granted"
	DigestConditional DigestState = "conditional"
)

// PermissionDigest is one user's structural grant enumeration for one scope:
// every resource and field the user's grants reach, each mapping permission
// to its DigestState. It is the payload of the frontend's per-scope
// permission digest — advisory UI material, never an enforcement surface.
//
// The digest is structural: it reports grant structure and folds nothing —
// no environment instant, no row data — so a payload is stable for the life
// of a policy snapshot and caches cleanly per scope. Absence means denied,
// and the payload stays proportional to the user's grants, never to the
// application's resource collection. Field targets appear under their dotted
// resource name ("employees.name"), exactly as grants store them.
type PermissionDigest map[Resource]map[Permission]DigestState
