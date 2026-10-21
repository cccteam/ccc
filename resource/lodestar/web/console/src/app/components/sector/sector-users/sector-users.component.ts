import { Component, computed, input } from '@angular/core';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { Domain, PermissionScope } from '@cccteam/resource';
import { HasPermissionDirective } from '@cccteam/resource-angular/auth-has-permission';
import { UserManagementComponent } from '@cccteam/resource-angular/ccc-user-management';

/**
 * The sector's crew page composes the library's user-management component over the
 * crew's partition in the sector the URL names (the people seated there, with their seats
 * there; a marshal seats every sector role but their own): the route's sector is the
 * component's domain, and nothing here reads the chart. The section is drawn only where
 * the sector's digest grants List on its users resource, so the marshal and headquarters
 * see it and a login without the grant there sees an empty page and nothing is requested.
 *
 * Demonstrates: auth.user-management.
 */
@Component({
  selector: 'app-sector-users',
  imports: [HasPermissionDirective, UserManagementComponent],
  templateUrl: './sector-users.component.html',
  styleUrl: './sector-users.component.scss',
})
export class SectorUsersComponent {
  /** The sector the URL names, bound from the route: the partition's domain. */
  sectorID = input.required<string>();

  sector = computed(() => this.sectorID() as Domain);

  /** The partition's question is asked in the route's sector's digest. */
  scope = computed<PermissionScope>(() => ({
    resource: Resources.CrewSectorUsers,
    permission: Permissions.List,
    domain: this.sector(),
  }));
}
