import { Component } from '@angular/core';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { PermissionScope } from '@cccteam/resource';
import { HasPermissionDirective } from '@cccteam/resource-angular/auth-has-permission';
import { UserManagementComponent } from '@cccteam/resource-angular/ccc-user-management';

/**
 * The Users page composes the library's user-management component over the global
 * partitions of the surfaces the console serves, and nothing here names a user, a role
 * or a route: the component reads the generated descriptor's surfaces and the digest
 * decides what each person may do. Two partitions: the crew's global partition (every
 * crew login, with the global and every-sector seats) and the members' global partition,
 * which the console manages for the portal's population (the generator's Manages
 * declaration). Each section is drawn only where the digest grants List on its users
 * resource, so headquarters sees both and a sector marshal, who holds nothing outside a
 * sector, sees neither. The crew's partition in a sector is the sector's own page, under
 * the sector's URL (SectorUsersComponent).
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
  readonly crewScope: PermissionScope = { resource: Resources.CrewUsers, permission: Permissions.List };
  readonly membersScope: PermissionScope = { resource: Resources.MembersUsers, permission: Permissions.List };
}
