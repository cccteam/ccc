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
	Prototype4 = 'Prototype4',
}

export enum Prototype1 {
	addr = 'Prototype1.addr',
	id = 'Prototype1.id',
}
export enum Prototype2 {
	socket = 'Prototype2.socket',
	sockopt = 'Prototype2.sockopt',
}
export enum Prototype3 {
	addr = 'Prototype3.addr',
	id = 'Prototype3.id',
}
export enum Prototype4 {
	socket = 'Prototype4.socket',
	sockopt = 'Prototype4.sockopt',
}

type AllResources = Resources | Prototype1 | Prototype2 | Prototype3 | Prototype4;
type PermissionResources = Record<Permissions, boolean>;
type PermissionMappings = Record<AllResources, PermissionResources>;

const Mappings: PermissionMappings = {
	[Resources.Prototype1]: {
		[Permissions.Create]:true,
		[Permissions.Delete]:true,
		[Permissions.List]:false,
		[Permissions.Read]:false,
		[Permissions.Update]:false,
	},
	[Prototype1.addr]: {
		[Permissions.Create]:false,
		[Permissions.Delete]:false,
		[Permissions.List]:false,
		[Permissions.Read]:true,
		[Permissions.Update]:false,
	},
	[Prototype1.id]: {
		[Permissions.Create]:true,
		[Permissions.Delete]:true,
		[Permissions.List]:false,
		[Permissions.Read]:false,
		[Permissions.Update]:false,
	},
	[Resources.Prototype2]: {
		[Permissions.Create]:false,
		[Permissions.Delete]:false,
		[Permissions.List]:true,
		[Permissions.Read]:true,
		[Permissions.Update]:true,
	},
	[Prototype2.socket]: {
		[Permissions.Create]:false,
		[Permissions.Delete]:false,
		[Permissions.List]:false,
		[Permissions.Read]:false,
		[Permissions.Update]:false,
	},
	[Prototype2.sockopt]: {
		[Permissions.Create]:false,
		[Permissions.Delete]:false,
		[Permissions.List]:true,
		[Permissions.Read]:true,
		[Permissions.Update]:false,
	},
	[Resources.Prototype3]: {
		[Permissions.Create]:true,
		[Permissions.Delete]:true,
		[Permissions.List]:false,
		[Permissions.Read]:false,
		[Permissions.Update]:false,
	},
	[Prototype3.addr]: {
		[Permissions.Create]:false,
		[Permissions.Delete]:false,
		[Permissions.List]:false,
		[Permissions.Read]:true,
		[Permissions.Update]:false,
	},
	[Prototype3.id]: {
		[Permissions.Create]:true,
		[Permissions.Delete]:true,
		[Permissions.List]:false,
		[Permissions.Read]:false,
		[Permissions.Update]:false,
	},
	[Resources.Prototype4]: {
		[Permissions.Create]:false,
		[Permissions.Delete]:false,
		[Permissions.List]:true,
		[Permissions.Read]:true,
		[Permissions.Update]:true,
	},
	[Prototype4.socket]: {
		[Permissions.Create]:false,
		[Permissions.Delete]:false,
		[Permissions.List]:false,
		[Permissions.Read]:false,
		[Permissions.Update]:false,
	},
	[Prototype4.sockopt]: {
		[Permissions.Create]:false,
		[Permissions.Delete]:false,
		[Permissions.List]:true,
		[Permissions.Read]:true,
		[Permissions.Update]:false,
	},
};

export function requiresPermission(resource: AllResources, permission: Permissions): boolean {
	return Mappings[resource][permission];
}
