import { Type } from '@angular/core';
import { Route } from '@angular/router';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { DomainRoutePath } from '@app/service/zz_gen_resources';
import { generatedNavItems } from '@cccteam/resource-angular/resource-nav';
import { MenuItem } from '@cccteam/resource-angular/types';
import { TenantSignInComponent } from './tenant-sign-in.component';

let cachedTenantSignInRoute: Route | undefined;

/**
 * tenantSignInRoute wires the tenant's sign-in page, who may enter the tenant as its
 * declaration says, under the tenant segment. Its navigation item names the segment's
 * route parameter, which the top bar fills with the open tenant, and is asked of that
 * tenant's digest for the List grant on its directories, so it is drawn only inside a
 * tenant where the grant holds.
 */
export const tenantSignInRoute = (): Route => {
  if (cachedTenantSignInRoute) {
    return cachedTenantSignInRoute;
  }

  const item: MenuItem = {
    label: 'Sign-in',
    route: [DomainRoutePath, 'sign-in'],
    permission: { resource: Resources.TenantDirectories, permission: Permissions.List },
  };
  generatedNavItems.push(item);

  cachedTenantSignInRoute = {
    path: 'sign-in',
    loadComponent: (): Promise<Type<TenantSignInComponent>> =>
      import('./tenant-sign-in.component').then((comp) => comp.TenantSignInComponent),
  };

  return cachedTenantSignInRoute;
};
