import { Component } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatMenuModule } from '@angular/material/menu';
import { RouterModule } from '@angular/router';
import { generatedNavItems } from '@cccteam/ccc-lib/resource-nav';

export interface MenuItem {
  label: string;
  route?: string[];
  children?: MenuItem[];
}

@Component({
  selector: 'app-topbar',
  imports: [MatButtonModule, MatMenuModule, RouterModule],
  templateUrl: './topbar.component.html',
  styleUrl: './topbar.component.scss',
})
export class TopbarComponent {
  menuData = generatedNavItems;
}
