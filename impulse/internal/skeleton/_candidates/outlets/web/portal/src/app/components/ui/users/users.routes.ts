import { Type } from '@angular/core';
import { Route } from '@angular/router';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { DomainRoutePath } from '@app/service/zz_gen_resources';
import { generatedNavItems } from '@cccteam/resource-angular/resource-nav';
import { MenuItem } from '@cccteam/resource-angular/types';
import { TenantUsersComponent } from './tenant-users.component';
import { UsersComponent } from './users.component';

let cachedUsersRoute: Route | undefined;
let cachedTenantUsersRoute: Route | undefined;

/**
 * usersRoute wires the Users page over the global partition of the members surface and
 * lists it in the generated navigation, beside the resource pages registered there,
 * behind the surface's List grant; the page itself draws the partition the digest grants.
 */
export const usersRoute = (): Route => {
  if (cachedUsersRoute) {
    return cachedUsersRoute;
  }

  const item: MenuItem = {
    label: 'Users',
    route: ['users'],
    permission: { resource: Resources.MembersUsers, permission: Permissions.List },
  };
  generatedNavItems.push(item);

  cachedUsersRoute = {
    path: 'users',
    loadComponent: (): Promise<Type<UsersComponent>> => import('./users.component').then((comp) => comp.UsersComponent),
  };

  return cachedUsersRoute;
};

/**
 * tenantUsersRoute wires the tenant's Users page, the members surface's partition in the
 * tenant the URL names, under the tenant segment. Its navigation item names the segment's
 * route parameter, which the top bar fills with the open tenant, and is asked of that
 * tenant's digest, so it is drawn only inside a tenant where the grant holds.
 */
export const tenantUsersRoute = (): Route => {
  if (cachedTenantUsersRoute) {
    return cachedTenantUsersRoute;
  }

  const item: MenuItem = {
    label: 'Tenant users',
    route: [DomainRoutePath, 'users'],
    permission: { resource: Resources.MembersTenantUsers, permission: Permissions.List },
  };
  generatedNavItems.push(item);

  cachedTenantUsersRoute = {
    path: 'users',
    loadComponent: (): Promise<Type<TenantUsersComponent>> =>
      import('./tenant-users.component').then((comp) => comp.TenantUsersComponent),
  };

  return cachedTenantUsersRoute;
};
