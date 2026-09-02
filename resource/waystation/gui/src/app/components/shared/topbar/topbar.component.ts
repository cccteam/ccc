import { Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatMenuModule } from '@angular/material/menu';
import { RouterModule } from '@angular/router';
import { HasPermissionDirective } from '@cccteam/ccc-lib/auth-has-permission';
import { generatedNavItems } from '@cccteam/ccc-lib/resource-nav';
import { PermissionScopes } from '@app/service/zz_gen_constants';
import { ResourceScopes } from '@app/service/zz_gen_resources';
import { Domain, MenuItem, PermissionScope, Resource } from '@cccteam/ccc-lib/types';
import { WaystationService } from '@components/waystation/waystation.service';

export type { MenuItem };

@Component({
  selector: 'app-topbar',
  imports: [MatButtonModule, MatMenuModule, RouterModule, HasPermissionDirective],
  templateUrl: './topbar.component.html',
  styleUrl: './topbar.component.scss',
})
export class TopbarComponent {
  private ws = inject(WaystationService);

  menuData = generatedNavItems;

  // scopeFor completes an item's permission question: a domain-scoped resource is
  // asked in the selected station's partition, a global one as is. Reads the
  // station signal, so the menu reshapes when the station changes.
  scopeFor(item: MenuItem): PermissionScope | undefined {
    const permission = item.permission;
    if (!permission || ResourceScopes[permission.resource as Resource] !== PermissionScopes.domain) {
      return permission;
    }
    const station = this.ws.current();
    return station ? { ...permission, domain: station as Domain } : permission;
  }
}
