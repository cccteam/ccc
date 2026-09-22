import { Component, computed, inject } from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { Resources } from '@app/service/zz_gen_constants';
import { ResourceScopes } from '@app/service/zz_gen_resources';
import { TenantService } from '@app/tenant/tenant.service';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { Permission, PermissionDigestState, Resource } from '@cccteam/resource';

/** One row of the permissions card: a resource and the digest state of each permission on it. */
interface PermissionRow {
  resource: string;
  scope: string;
  states: Record<string, PermissionDigestState | undefined>;
}

/**
 * The dashboard shows who is signed in and what the session holds: per generated
 * resource, the digest state of each permission — in the selected tenant for
 * tenant-scoped resources, globally for global ones.
 */
@Component({
  selector: 'app-dashboard',
  imports: [MatCardModule],
  templateUrl: './dashboard.component.html',
  styleUrl: './dashboard.component.scss',
})
export class DashboardComponent {
  auth = inject(AuthService);
  tenants = inject(TenantService);

  readonly permissionNames = ['List', 'Read', 'Create', 'Update', 'Delete', 'Execute'] as Permission[];

  rows = computed<PermissionRow[]>(() => {
    const rows: PermissionRow[] = [];
    for (const resource of Object.values(Resources)) {
      const states: PermissionRow['states'] = {};
      let held = false;
      for (const permission of this.permissionNames) {
        const state = this.tenants.state(permission, resource);
        states[permission] = state;
        held ||= state !== undefined;
      }
      if (held) {
        rows.push({ resource, scope: this.scopeOf(resource), states });
      }
    }
    return rows;
  });

  private scopeOf(resource: Resource): string {
    return ResourceScopes[resource] === 'domain' ? this.tenants.current() : 'global';
  }
}
