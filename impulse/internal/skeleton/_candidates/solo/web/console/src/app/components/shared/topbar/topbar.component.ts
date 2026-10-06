import { Component } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatMenuModule } from '@angular/material/menu';
import { RouterModule } from '@angular/router';
import { HasPermissionDirective } from '@cccteam/resource-angular/auth-has-permission';
import { generatedNavItems } from '@cccteam/resource-angular/resource-nav';

/**
 * The navigation menu over the generated resource routes. Items carry the permission
 * their destination requires; cccHasPermission answers from the permission digest, so a
 * user sees only the menus they can open.
 */
@Component({
  selector: 'app-topbar',
  imports: [MatButtonModule, MatMenuModule, RouterModule, HasPermissionDirective],
  templateUrl: './topbar.component.html',
  styleUrl: './topbar.component.scss',
})
export class TopbarComponent {
  menuData = generatedNavItems;
}
