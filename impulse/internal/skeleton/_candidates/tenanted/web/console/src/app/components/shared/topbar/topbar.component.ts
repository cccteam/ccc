import { Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatMenuModule } from '@angular/material/menu';
import { RouterModule } from '@angular/router';
import { PermissionScopes } from '@app/service/zz_gen_constants';
import { ResourceScopes } from '@app/service/zz_gen_resources';
import { TenantService } from '@app/tenant/tenant.service';
import { HasPermissionDirective } from '@cccteam/resource-angular/auth-has-permission';
import { generatedNavItems } from '@cccteam/resource-angular/resource-nav';
import { Domain, MenuItem, PermissionScope, Resource } from '@cccteam/resource-angular/types';

/**
 * The navigation menu over the generated resource routes. Items carry the permission
 * their destination requires; cccHasPermission answers from the permission digest, so a
 * user sees only the menus they can open.
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
  // in the selected tenant's partition, a global one as is. Reads the tenant signal, so
  // the menu reshapes when the tenant changes.
  scopeFor(item: MenuItem): PermissionScope | undefined {
    const permission = item.permission;
    if (!permission || ResourceScopes[permission.resource as Resource] !== PermissionScopes.domain) {
      return permission;
    }
    const tenant = this.tenants.current();
    return tenant ? { ...permission, domain: tenant as Domain } : permission;
  }
}
