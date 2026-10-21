import { Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatMenuModule } from '@angular/material/menu';
import { RouterModule } from '@angular/router';
import { PermissionScopes } from '@app/service/zz_gen_constants';
import { DomainRouteParam, ResourceScopes } from '@app/service/zz_gen_resources';
import { TenantService } from '@app/tenant/tenant.service';
import { HasPermissionDirective } from '@cccteam/resource-angular/auth-has-permission';
import { generatedNavItems } from '@cccteam/resource-angular/resource-nav';
import { Domain, MenuItem, PermissionScope, Resource } from '@cccteam/resource-angular/types';

/**
 * The navigation menu over the generated resource routes. Items carry the permission
 * their destination requires; cccHasPermission answers from the permission digest, so a
 * user sees only the menus they can open. An item under the tenant segment names the
 * segment's route parameter, which the open tenant fills, so its link lands under the
 * tenant the page is in; outside a tenant such items are not drawn, and the picker is the
 * way in.
 */
@Component({
  selector: 'app-topbar',
  imports: [MatButtonModule, MatMenuModule, RouterModule, HasPermissionDirective],
  templateUrl: './topbar.component.html',
  styleUrl: './topbar.component.scss',
})
export class TopbarComponent {
  private tenants = inject(TenantService);

  menuData = generatedNavItems;

  // scopeFor completes an item's permission question: a tenant-scoped resource is asked
  // in the open tenant's partition, a global one as is. Reads the tenant signal, so the
  // menu reshapes when the tenant changes.
  scopeFor(item: MenuItem): PermissionScope | undefined {
    const permission = item.permission;
    if (!permission || ResourceScopes[permission.resource as Resource] !== PermissionScopes.domain) {
      return permission;
    }
    const tenant = this.tenants.current();
    return tenant ? { ...permission, domain: tenant as Domain } : permission;
  }

  // linkFor completes an item's route: the segment's parameter, where the route names
  // it, is the open tenant; undefined while no tenant is open, so the item is not drawn.
  linkFor(item: MenuItem): string[] | undefined {
    const route = item.route;
    const parameter = `:${DomainRouteParam}`;
    if (!route?.some((part) => part.includes(parameter))) {
      return route;
    }
    const tenant = this.tenants.current();
    return tenant ? route.map((part) => part.replace(parameter, tenant)) : undefined;
  }
}
