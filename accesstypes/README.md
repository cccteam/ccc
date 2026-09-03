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
