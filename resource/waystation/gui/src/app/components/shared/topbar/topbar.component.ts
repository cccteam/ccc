import { Component } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatMenuModule } from '@angular/material/menu';
import { RouterModule } from '@angular/router';
import { HasPermissionDirective } from '@cccteam/ccc-lib/auth-has-permission';
import { generatedNavItems } from '@cccteam/ccc-lib/resource-nav';
import { MenuItem } from '@cccteam/ccc-lib/types';

export type { MenuItem };

@Component({
  selector: 'app-topbar',
  imports: [MatButtonModule, MatMenuModule, RouterModule, HasPermissionDirective],
  templateUrl: './topbar.component.html',
  styleUrl: './topbar.component.scss',
})
export class TopbarComponent {
  menuData = generatedNavItems;
}
