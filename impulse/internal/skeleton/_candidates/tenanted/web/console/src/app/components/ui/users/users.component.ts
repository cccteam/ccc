import { Component, computed, inject } from '@angular/core';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { TenantService } from '@app/tenant/tenant.service';
import { Domain, PermissionScope } from '@cccteam/resource';
import { HasPermissionDirective } from '@cccteam/resource-angular/auth-has-permission';
import { UserManagementComponent } from '@cccteam/resource-angular/ccc-user-management';

/**
 * The Users page composes the library's user-management component over the staff auth's
 * user surface, and nothing here names a user, a role or a route: the component reads the
 * generated descriptor's surface and the permission digest decides what each person may
 * do. Two partitions: the global one (every staff login, with the global seats) and the
 * one of the tenant selected in the header (the people seated there, with their seats
 * there). Each section is drawn only where the digest grants List on its users resource,
 * so a tenant's administrator sees the tenant's partition and a global administrator both.
 */
@Component({
  selector: 'app-users',
  imports: [HasPermissionDirective, UserManagementComponent],
  templateUrl: './users.component.html',
  styleUrl: './users.component.scss',
})
export class UsersComponent {
  private tenants = inject(TenantService);

  /** The selected tenant as the tenant partition's domain; undefined while none is selected. */
  tenant = computed(() => (this.tenants.current() || undefined) as Domain | undefined);

  readonly staffScope: PermissionScope = { resource: Resources.StaffUsers, permission: Permissions.List };
  /** The tenant partition's question is asked in the selected tenant's digest. */
  tenantScope = computed<PermissionScope | undefined>(() => {
    const tenant = this.tenant();
    return tenant ? { resource: Resources.StaffTenantUsers, permission: Permissions.List, domain: tenant } : undefined;
  });
}
