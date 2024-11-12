// This file is auto-generated. Do not edit manually.
export enum Permissions {
	Create = 'Create',
	Delete = 'Delete',
	List = 'List',
	Read = 'Read',
	Update = 'Update',
}
export enum Resources {
	Prototype1 = 'Prototype1',
	Prototype2 = 'Prototype2',
	Prototype3 = 'Prototype3',
}
export enum Prototype1 {
	id = 'Prototype1.id',
	protocol = 'Prototype1.protocol',
}
export enum Prototype2 {
	addr = 'Prototype2.addr',
	id = 'Prototype2.id',
	uuid = 'Prototype2.uuid',
}
export enum Prototype3 {
	socket = 'Prototype3.socket',
	sockopt = 'Prototype3.sockopt',
}
type AllResources = Resources | Prototype1 | Prototype2 | Prototype3;
type PermissionResources = Record<Permissions, boolean>;
type PermissionMappings = Record<AllResources, PermissionResources>;
const Mappings: PermissionMappings = {
	[Resources.Prototype1]: {
		[Permissions.Create]:false,
		[Permissions.Delete]:false,
		[Permissions.List]:true,
		[Permissions.Read]:true,
		[Permissions.Update]:true,
	},
	[Prototype1.id]: {
		[Permissions.Create]:false,
		[Permissions.Delete]:false,
		[Permissions.List]:true,
		[Permissions.Read]:true,
		[Permissions.Update]:true,
	},
	[Prototype1.protocol]: {
		[Permissions.Create]:false,
		[Permissions.Delete]:false,
		[Permissions.List]:true,
		[Permissions.Read]:true,
		[Permissions.Update]:true,
	},
	[Resources.Prototype2]: {
		[Permissions.Create]:true,
		[Permissions.Delete]:true,
		[Permissions.List]:false,
		[Permissions.Read]:false,
		[Permissions.Update]:false,
	},
	[Prototype2.addr]: {
		[Permissions.Create]:false,
		[Permissions.Delete]:false,
		[Permissions.List]:true,
		[Permissions.Read]:true,
		[Permissions.Update]:true,
	},
	[Prototype2.id]: {
		[Permissions.Create]:false,
		[Permissions.Delete]:false,
		[Permissions.List]:true,
		[Permissions.Read]:true,
		[Permissions.Update]:true,
	},
	[Prototype2.uuid]: {
		[Permissions.Create]:true,
		[Permissions.Delete]:false,
		[Permissions.List]:false,
		[Permissions.Read]:false,
		[Permissions.Update]:false,
	},
	[Resources.Prototype3]: {
		[Permissions.Create]:true,
		[Permissions.Delete]:false,
		[Permissions.List]:false,
		[Permissions.Read]:false,
		[Permissions.Update]:true,
	},
	[Prototype3.socket]: {
		[Permissions.Create]:true,
		[Permissions.Delete]:true,
		[Permissions.List]:true,
		[Permissions.Read]:true,
		[Permissions.Update]:true,
	},
	[Prototype3.sockopt]: {
		[Permissions.Create]:true,
		[Permissions.Delete]:true,
		[Permissions.List]:false,
		[Permissions.Read]:false,
		[Permissions.Update]:true,
	},
};
export function requiresPermission(resource: AllResources, permission: Permissions): boolean {
	return Mappings[resource][permission];
}
