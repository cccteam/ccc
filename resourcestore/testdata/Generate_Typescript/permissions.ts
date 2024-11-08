// This file is auto-generated. Do not edit manually.

export enum Permissions {
  Create = "Create",
  Delete = "Delete",
  List = "List",
  Read = "Read",
  Update = "Update",
}

export enum Resources {
  Prototypes = "Prototypes",
}
export enum Prototypes {
  str2 = "Prototypes.str2",
  int1 = "Prototypes.int1",
  time1 = "Prototypes.time1",
  time2 = "Prototypes.time2",
  int2 = "Prototypes.int2",
  id = "Prototypes.id",
  uuid1 = "Prototypes.uuid1",
  uuid2 = "Prototypes.uuid2",
  str1 = "Prototypes.str1",
}

type AllResources = Resources | Prototypes;
type PermissionResources = Record<Permissions, boolean>;
type PermissionMappings = Record<AllResources, PermissionResources>;

const Mappings: PermissionMappings = {
  [Resources.Prototypes]: {
    [Permissions.Create]: true,
    [Permissions.Delete]: false,
    [Permissions.List]: false,
    [Permissions.Read]: false,
    [Permissions.Update]: false,
  },
  [Prototypes.str2]: {
    [Permissions.Create]: true,
    [Permissions.Delete]: false,
    [Permissions.List]: false,
    [Permissions.Read]: false,
    [Permissions.Update]: false,
  },
  [Prototypes.int1]: {
    [Permissions.Create]: true,
    [Permissions.Delete]: false,
    [Permissions.List]: false,
    [Permissions.Read]: false,
    [Permissions.Update]: false,
  },
  [Prototypes.time1]: {
    [Permissions.Create]: true,
    [Permissions.Delete]: false,
    [Permissions.List]: false,
    [Permissions.Read]: false,
    [Permissions.Update]: false,
  },
  [Prototypes.time2]: {
    [Permissions.Create]: true,
    [Permissions.Delete]: false,
    [Permissions.List]: false,
    [Permissions.Read]: false,
    [Permissions.Update]: false,
  },
  [Prototypes.int2]: {
    [Permissions.Create]: true,
    [Permissions.Delete]: false,
    [Permissions.List]: false,
    [Permissions.Read]: false,
    [Permissions.Update]: false,
  },
  [Prototypes.id]: {
    [Permissions.Create]: true,
    [Permissions.Delete]: false,
    [Permissions.List]: false,
    [Permissions.Read]: false,
    [Permissions.Update]: false,
  },
  [Prototypes.uuid1]: {
    [Permissions.Create]: true,
    [Permissions.Delete]: false,
    [Permissions.List]: false,
    [Permissions.Read]: false,
    [Permissions.Update]: false,
  },
  [Prototypes.uuid2]: {
    [Permissions.Create]: true,
    [Permissions.Delete]: false,
    [Permissions.List]: false,
    [Permissions.Read]: false,
    [Permissions.Update]: false,
  },
  [Prototypes.str1]: {
    [Permissions.Create]: true,
    [Permissions.Delete]: false,
    [Permissions.List]: false,
    [Permissions.Read]: false,
    [Permissions.Update]: false,
  },
};

export function requiresPermission(
  resource: AllResources,
  permission: Permissions
): boolean {
  return Mappings[resource][permission];
}
