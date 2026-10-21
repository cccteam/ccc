import { Component, computed, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { RouterModule } from '@angular/router';
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
 * resource, the digest state of each permission — in the tenant the page is open in for
 * tenant-scoped resources, globally for global ones. It lives outside the tenant segment
 * and under it: the landing for a session that has picked no tenant, and the tenant's.
 * A session holding no tenant is offered the create-your-tenant page instead: the Tenant
 * record's creation shape (`@tenant(creation: both)`) lets a signed-in person create
 * their own.
 */
@Component({
  selector: 'app-dashboard',
  imports: [MatButtonModule, MatCardModule, RouterModule],
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
