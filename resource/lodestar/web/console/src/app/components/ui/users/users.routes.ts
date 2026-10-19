import { Type } from '@angular/core';
import { Route } from '@angular/router';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { generatedNavItems } from '@cccteam/resource-angular/resource-nav';
import { MenuItem } from '@shared/topbar/topbar.component';
import { UsersComponent } from './users.component';

let cachedUsersRoute: Route | undefined;

/**
 * usersRoute wires the Users page and lists it in the Headquarters group of the generated
 * navigation, beside the config-driven pages registered there, behind the crew surface's
 * List grant; the page itself draws each partition the digest grants.
 */
export const usersRoute = (): Route => {
  if (cachedUsersRoute) {
    return cachedUsersRoute;
  }

  let group = generatedNavItems.find((item) => item.label === 'Headquarters');
  if (!group) {
    group = { label: 'Headquarters', children: [] };
    generatedNavItems.push(group);
  }
  const item: MenuItem = {
    label: 'Users',
    route: ['users'],
    permission: { resource: Resources.CrewUsers, permission: Permissions.List },
  };
  group.children = [...(group.children ?? []), item];

  cachedUsersRoute = {
    path: 'users',
    loadComponent: (): Promise<Type<UsersComponent>> => import('./users.component').then((comp) => comp.UsersComponent),
  };

  return cachedUsersRoute;
};
