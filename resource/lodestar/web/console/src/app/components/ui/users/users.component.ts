import { Component, computed, inject } from '@angular/core';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { SectorService } from '@components/sector/sector.service';
import { Domain, PermissionScope } from '@cccteam/resource';
import { HasPermissionDirective } from '@cccteam/resource-angular/auth-has-permission';
import { UserManagementComponent } from '@cccteam/resource-angular/ccc-user-management';

/**
 * The Users page composes the library's user-management component over the surfaces the
 * console serves, and nothing here names a user, a role or a route: the component reads
 * the generated descriptor's surfaces and the digest decides what each person may do.
 * Three partitions: the crew's global partition (every crew login, with the global and
 * every-sector seats), the crew's partition in the selected sector (the people seated
 * there, with their seats there; a marshal seats every sector role but their own), and
 * the members' global partition, which the console manages for the portal's population
 * (the generator's Manages declaration). Each section is drawn only where the digest
 * grants List on its users resource, so a sector marshal sees the sector's partition and
 * headquarters sees all three.
 *
 * Demonstrates: auth.user-management.
 */
@Component({
  selector: 'app-users',
  imports: [HasPermissionDirective, UserManagementComponent],
  templateUrl: './users.component.html',
  styleUrl: './users.component.scss',
})
export class UsersComponent {
  private sectors = inject(SectorService);

  /** The selected sector as the tenant partition's domain; undefined while none is selected. */
  sector = computed(() => (this.sectors.current() || undefined) as Domain | undefined);

  readonly crewScope: PermissionScope = { resource: Resources.CrewUsers, permission: Permissions.List };
  readonly membersScope: PermissionScope = { resource: Resources.MembersUsers, permission: Permissions.List };
  /** The sector partition's question is asked in the selected sector's digest. */
  sectorScope = computed<PermissionScope | undefined>(() => {
    const sector = this.sector();
    return sector ? { resource: Resources.CrewSectorUsers, permission: Permissions.List, domain: sector } : undefined;
  });
}
