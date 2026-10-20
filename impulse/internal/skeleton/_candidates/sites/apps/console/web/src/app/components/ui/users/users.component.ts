import { Component } from '@angular/core';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { PermissionScope } from '@cccteam/resource';
import { HasPermissionDirective } from '@cccteam/resource-angular/auth-has-permission';
import { UserManagementComponent } from '@cccteam/resource-angular/ccc-user-management';

/**
 * The Users page composes the library's user-management component over the staff auth's
 * user surface, and nothing here names a user, a role or a route: the component reads the
 * generated descriptor's surface and the permission digest decides what each person may
 * do. The section is drawn only where the digest grants List on the users resource, so a
 * person without the grant sees an empty page and nothing is requested.
 */
@Component({
  selector: 'app-users',
  imports: [HasPermissionDirective, UserManagementComponent],
  templateUrl: './users.component.html',
  styleUrl: './users.component.scss',
})
export class UsersComponent {
  readonly staffScope: PermissionScope = { resource: Resources.StaffUsers, permission: Permissions.List };
}
