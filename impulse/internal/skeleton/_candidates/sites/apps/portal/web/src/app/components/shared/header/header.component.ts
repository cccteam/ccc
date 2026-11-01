import { Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatDialog } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatSelectModule } from '@angular/material/select';
import { RouterModule } from '@angular/router';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { TenantService } from '@app/tenant/tenant.service';
import { PermissionScope } from '@cccteam/resource';
import { HasPermissionDirective } from '@cccteam/resource-angular/auth-has-permission';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { openFeatureFlagsDialog } from '@cccteam/resource-angular/ccc-feature-flags';
import { TopbarComponent } from '../topbar/topbar.component';

@Component({
  selector: 'app-header',
  imports: [HasPermissionDirective, MatButtonModule, MatFormFieldModule, MatSelectModule, RouterModule, TopbarComponent],
  templateUrl: './header.component.html',
  styleUrls: ['./header.component.scss'],
})
export class HeaderComponent {
  auth = inject(AuthService);
  tenants = inject(TenantService);
  private dialog = inject(MatDialog);

  /**
   * The grant that shows the Feature flags link: List on the flags, which is what the
   * library's dialog reads. Whether the dialog may flip one is the digest's Execute on
   * SetFeature, which the dialog asks for itself; with List alone it is read-only.
   */
  readonly flagsScope: PermissionScope = { resource: Resources.FeatureFlags, permission: Permissions.List };

  /** Opens the library's feature flags dialog over the application. */
  openFeatureFlags(): void {
    openFeatureFlagsDialog(this.dialog);
  }
}
