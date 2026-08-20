# accesstypes

The `accesstypes` package provides types used by the `access` package and other
dependent packages.

It is a zero-dependency vocabulary package: the string-based identity types
(`Permission`, `Role`, `Domain`, `Resource`, `User`, `Tag`, `Field`) shared by the
`access` package (casbin-backed authorization), `ccc/resource`, and the generated
permission collections. It contains no logic beyond prefix marshaling and
resource/tag composition — it is the common language for authorization, so
dependent packages should use these types rather than re-declaring their own.

## Core types

All five identity types follow one shape — `type X string`, an `UnmarshalX` that
strips a prefix, and a `Marshal()` that adds it. Casbin stores flat strings; these
prefixes namespace them.

| Type | Prefix | Round-trip |
| --- | --- | --- |
| `Domain` | `domain:` | `UnmarshalDomain("domain:acme")` ⇄ `Domain("acme").Marshal()` |
| `Role` | `role:` | `UnmarshalRole` ⇄ `Role.Marshal()` |
| `User` | `user:` | `UnmarshalUser` ⇄ `User.Marshal()` |
| `Resource` | `resource:` | `UnmarshalResource` ⇄ `Resource.Marshal()` |
| `Permission` | `perm:` | `UnmarshalPermission` ⇄ `Permission.Marshal()` |

Supporting types: `Tag` (a field's json tag name), `Field` (a Go struct field
name), and `PermissionScope` (`GlobalPermissionScope` / `DomainPermissionScope`).

## Constants

```go
accesstypes.GlobalDomain   // Domain("global") — permission applies across domains
accesstypes.GlobalResource // Resource("global") — permission applies app-wide
accesstypes.NoopUser       // "noop" — assigned to empty casbin roles so they stay enumerable

// CRUD/RPC permissions
accesstypes.NullPermission // ""
accesstypes.Create, accesstypes.Read, accesstypes.List
accesstypes.Update, accesstypes.Delete
accesstypes.Execute        // used for RPC methods
```

`Read` means fetch one; `List` means fetch many. `NoopUser` is an untyped string
constant, not a `User` — it assigns implicitly, but `NoopUser.Marshal()` does not
compile.

## Field-level resources

Compose and split `Resource.Tag` pairs instead of concatenating strings:

```go
fieldRes := accesstypes.Resource("Widgets").ResourceWithTag("code") // "Widgets.code"
res, tag := fieldRes.ResourceAndTag()                               // "Widgets", "code"
```

`ResourceAndTag` returns `(resource, "")` when there is no tag.

## Permission maps

The package predefines the map shapes used throughout the stack:

```go
type TagPermissions map[Tag][]Permission                          // per-field requirements
type ResolvedTagPermissions map[Domain]map[Resource]map[Tag]map[Permission]bool
type ResolvedResourcePermissions map[Domain]map[Resource]map[Permission]bool
type ResolvedPermissions struct {
    Resources ResolvedResourcePermissions
    Tags      ResolvedTagPermissions
}
type PermissionDetail struct { Description string; Scope PermissionScope }
type RoleCollection map[Domain][]Role
type RolePermissionCollection map[Permission][]Resource
type UserPermissionCollection map[Domain]map[Resource][]Permission
```

Declaring field requirements looks like:

```go
required := accesstypes.TagPermissions{
    "name": {accesstypes.Read},
    "code": {accesstypes.Create, accesstypes.Update},
}
```

## Implementing a permission checker

Consumers (e.g. `ccc/resource`) expect this shape — see
`resource/starport/integration/harness_test.go` for the canonical example:

```go
func (s *userPermissions) Check(ctx context.Context, perm accesstypes.Permission,
    resources ...accesstypes.Resource) (ok bool, missing []accesstypes.Resource, err error)
func (s *userPermissions) Domain() accesstypes.Domain
func (s *userPermissions) User() accesstypes.User
```

## Notes

- **`Marshal()` panics on already-prefixed values** — `Domain("domain:x").Marshal()`
  panics. Marshaling is not idempotent; callers must track whether a string is
  already marshaled.
- **`ResourceWithTag` panics** if the tag contains `.`; **`ResourceAndTag` panics**
  if the resource contains more than one `.`.
- Validation is prefix-only: empty strings and odd characters are accepted, and
  `NullPermission` happily marshals to `"perm:"`.
- `GlobalDomain`/`GlobalResource` are literally `"global"` — never name a real
  domain or resource `global`.
- These are defined string types, not aliases. Untyped literals assign freely
  (`var p accesstypes.Permission = "Read"`), while typed strings require
  conversion; prefer the constants.
