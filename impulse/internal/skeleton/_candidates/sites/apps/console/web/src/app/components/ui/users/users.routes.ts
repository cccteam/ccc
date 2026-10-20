import { Type } from '@angular/core';
import { Route } from '@angular/router';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { generatedNavItems } from '@cccteam/resource-angular/resource-nav';
import { MenuItem } from '@cccteam/resource-angular/types';
import { UsersComponent } from './users.component';

let cachedUsersRoute: Route | undefined;

/**
 * usersRoute wires the Users page and lists it in the generated navigation, beside the
 * resource pages registered there, behind the staff surface's List grant; the page itself
 * draws each partition the digest grants.
 */
export const usersRoute = (): Route => {
  if (cachedUsersRoute) {
    return cachedUsersRoute;
  }

  const item: MenuItem = {
    label: 'Users',
    route: ['users'],
    permission: { resource: Resources.StaffUsers, permission: Permissions.List },
  };
  generatedNavItems.push(item);

  cachedUsersRoute = {
    path: 'users',
    loadComponent: (): Promise<Type<UsersComponent>> => import('./users.component').then((comp) => comp.UsersComponent),
  };

  return cachedUsersRoute;
};
