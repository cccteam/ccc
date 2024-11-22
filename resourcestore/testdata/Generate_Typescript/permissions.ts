// This file is auto-generated. Do not edit manually.
import { Domain, Permission, Resource } from '@cccteam/ccc-types';
export const Permissions = {
	Create: 'Create' as Permission,
	Delete: 'Delete' as Permission,
	List: 'List' as Permission,
	Read: 'Read' as Permission,
	Update: 'Update' as Permission,
};

export const Domains = {
	global: 'global' as Domain,
	domain: 'domain' as Domain,
};

export const Resources = {
	Prototype1: 'Prototype1' as Resource,
	Prototype2: 'Prototype2' as Resource,
	Prototype3: 'Prototype3' as Resource,
	Prototype4: 'Prototype4' as Resource,
};

export const Prototype1 = {
	addr: 'Prototype1.addr' as Resource,
	id: 'Prototype1.id' as Resource,
};

export const Prototype2 = {
	socket: 'Prototype2.socket' as Resource,
	sockopt: 'Prototype2.sockopt' as Resource,
};

export const Prototype3 = {
	addr: 'Prototype3.addr' as Resource,
	id: 'Prototype3.id' as Resource,
};

export const Prototype4 = {
	socket: 'Prototype4.socket' as Resource,
	sockopt: 'Prototype4.sockopt' as Resource,
};

type PermissionResources = Record<Permission, boolean>;
type PermissionMappings = Record<Resource, PermissionResources>;

const Mappings: PermissionMappings = {
	[Resources.Prototype1]: {
		[Permissions.Create]: true,
		[Permissions.Delete]: true,
		[Permissions.List]: false,
		[Permissions.Read]: false,
		[Permissions.Update]: false,
	},
	[Prototype1.addr]: {
		[Permissions.Create]: false,
		[Permissions.Delete]: false,
		[Permissions.List]: false,
		[Permissions.Read]: true,
		[Permissions.Update]: false,
	},
	[Prototype1.id]: {
		[Permissions.Create]: true,
		[Permissions.Delete]: true,
		[Permissions.List]: false,
		[Permissions.Read]: false,
		[Permissions.Update]: false,
	},
	[Resources.Prototype2]: {
		[Permissions.Create]: false,
		[Permissions.Delete]: false,
		[Permissions.List]: true,
		[Permissions.Read]: true,
		[Permissions.Update]: true,
	},
	[Prototype2.socket]: {
		[Permissions.Create]: false,
		[Permissions.Delete]: false,
		[Permissions.List]: false,
		[Permissions.Read]: false,
		[Permissions.Update]: false,
	},
	[Prototype2.sockopt]: {
		[Permissions.Create]: false,
		[Permissions.Delete]: false,
		[Permissions.List]: true,
		[Permissions.Read]: true,
		[Permissions.Update]: false,
	},
	[Resources.Prototype3]: {
		[Permissions.Create]: true,
		[Permissions.Delete]: true,
		[Permissions.List]: false,
		[Permissions.Read]: false,
		[Permissions.Update]: false,
	},
	[Prototype3.addr]: {
		[Permissions.Create]: false,
		[Permissions.Delete]: false,
		[Permissions.List]: false,
		[Permissions.Read]: true,
		[Permissions.Update]: false,
	},
	[Prototype3.id]: {
		[Permissions.Create]: true,
		[Permissions.Delete]: true,
		[Permissions.List]: false,
		[Permissions.Read]: false,
		[Permissions.Update]: false,
	},
	[Resources.Prototype4]: {
		[Permissions.Create]: false,
		[Permissions.Delete]: false,
		[Permissions.List]: true,
		[Permissions.Read]: true,
		[Permissions.Update]: true,
	},
	[Prototype4.socket]: {
		[Permissions.Create]: false,
		[Permissions.Delete]: false,
		[Permissions.List]: false,
		[Permissions.Read]: false,
		[Permissions.Update]: false,
	},
	[Prototype4.sockopt]: {
		[Permissions.Create]: false,
		[Permissions.Delete]: false,
		[Permissions.List]: true,
		[Permissions.Read]: true,
		[Permissions.Update]: false,
	},
};

export function requiresPermission(resource: Resource, permission: Permission): boolean {
	return Mappings[resource][permission];
}
