import { Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatMenuModule } from '@angular/material/menu';
import { RouterModule } from '@angular/router';
import { HasPermissionDirective } from '@cccteam/ccc-lib/auth-has-permission';
import { generatedNavItems } from '@cccteam/ccc-lib/resource-nav';
import { PermissionScopes } from '@app/service/zz_gen_constants';
import { ResourceScopes } from '@app/service/zz_gen_resources';
import { Domain, MenuItem, PermissionScope, Resource } from '@cccteam/ccc-lib/types';
import { SectorService } from '@components/sector/sector.service';

export type { MenuItem };

@Component({
  selector: 'app-topbar',
  imports: [MatButtonModule, MatMenuModule, RouterModule, HasPermissionDirective],
  templateUrl: './topbar.component.html',
  styleUrl: './topbar.component.scss',
})
export class TopbarComponent {
  private sectors = inject(SectorService);

  menuData = generatedNavItems;

  // scopeFor completes an item's permission question: a sector-scoped resource is
  // asked in the selected sector's partition, a global one as is.
  scopeFor(item: MenuItem): PermissionScope | undefined {
    const permission = item.permission;
    if (!permission || ResourceScopes[permission.resource as Resource] !== PermissionScopes.domain) {
      return permission;
    }
    const sector = this.sectors.current();
    return sector ? { ...permission, domain: sector as Domain } : permission;
  }
}
