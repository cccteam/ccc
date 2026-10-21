import { Component, computed, input } from '@angular/core';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { Domain, PermissionScope } from '@cccteam/resource';
import { HasPermissionDirective } from '@cccteam/resource-angular/auth-has-permission';
import { UserManagementComponent } from '@cccteam/resource-angular/ccc-user-management';

/**
 * The tenant's Users page composes the library's user-management component over the
 * staff auth's partition in the tenant the URL names (the people seated there, with their
 * seats there): the route's tenant is the component's domain, and nothing here reads the
 * picker. The section is drawn only where the tenant's digest grants List on its users
 * resource, so a tenant's administrator sees it and a login without the grant sees an
 * empty page and nothing is requested.
 */
@Component({
  selector: 'app-tenant-users',
  imports: [HasPermissionDirective, UserManagementComponent],
  templateUrl: './tenant-users.component.html',
  styleUrl: './users.component.scss',
})
export class TenantUsersComponent {
  /** The tenant the URL names, bound from the route: the partition's domain. */
  tenantID = input.required<string>();

  tenant = computed(() => this.tenantID() as Domain);

  /** The partition's question is asked in the route's tenant's digest. */
  scope = computed<PermissionScope>(() => ({
    resource: Resources.StaffTenantUsers,
    permission: Permissions.List,
    domain: this.tenant(),
  }));
}
