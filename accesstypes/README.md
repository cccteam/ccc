# accesstypes

The `accesstypes` package provides types used by the `access` package and other dependent packages.

## Principals and masks

`Principal` is the authorization subject a session evaluates against — a user
(`UserPrincipal`) or a role (`RolePrincipal`). Kind is structural: the
constructors set an unexported discriminator, so no username can read as a role
and no role name as a user. Whether a session is impersonated is a property of
the session's impersonation record, never of the `Principal` value.

`PermissionMask` is the allowlist intersection an impersonated session carries
over the permission axis. The zero mask is unrestricted; `MaskPermissions(List,
Read)` allows exactly those permissions. A mask only narrows — a masked check
asks the mask before it asks policy, so nothing a mask does can grant what
policy denies. `Permissions()` is the persistence form: `nil` for unrestricted,
a sorted allowlist otherwise.

## Scopes: where a request is, and where policy is held

`Scope` is the partition an operation applies to: the global partition
(`GlobalScope()`) or one tenant domain (`DomainScope(domain)`). A permission check,
a permission digest and the foothold question take a `Scope`, because a request is
in exactly one partition. Global-ness is structural, an unexported flag, so no
domain string reads as the global partition: a tenant literally named `global` is
an ordinary tenant.

`PolicyScope` is where policy is held: a role membership, a custom role and its
grants live in the global partition (`GlobalPolicyScope()`), in one tenant domain
(`DomainPolicyScope(domain)`), or in every tenant domain
(`EveryDomainPolicyScope()`). The policy store, the user manager and the login's
role synchronization take a `PolicyScope`. A membership held in every domain
reaches every tenant scope, including a tenant the store holds no other row in, so
a tenant created at run time needs nothing written for its every-domain members.

The two types are kept apart on purpose. "Every domain" is not a place a request
can be in, so a `PolicyScope` cannot be passed where a `Scope` is expected and
nothing converts one into a `Scope`; an invalid call fails to compile rather than
at run time. `Scope.PolicyScope()` converts the other way, since every partition a
request can be in is also a place policy can be held, and `PolicyScope.Covers(scope)`
answers whether policy held there applies in a partition.
