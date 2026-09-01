import { Routes } from '@angular/router';
import { resourceMeta } from '@app/service/zz_gen_resources';
import { LoginAuthenticationGuard } from '@cccteam/ccc-lib/auth-authentication-guard';
import { resourceRoutes } from '@cccteam/ccc-lib/resource-route-generator';
import { waystationRoute } from '@components/waystation/waystation.routes';
import { UiComponent } from '@components/ui/ui.component';
import { catalogItemsConfig } from './configs/catalogItems.config';
import { staffMembersConfig } from './configs/staffMembers.config';
import { suppliersConfig } from './configs/suppliers.config';
import { waystationsConfig } from './configs/waystations.config';

export const routes: Routes = [
  {
    path: 'login',
    loadComponent: () => import('./components/login/login.component').then((comp) => comp.LoginComponent),
  },
  {
    path: '',
    component: UiComponent,
    canActivate: [LoginAuthenticationGuard],
    children: [
      {
        path: 'dashboard',
        loadComponent: () =>
          import('./components/ui/dashboard/dashboard.component').then((comp) => comp.DashboardComponent),
      },
      // Global resources are config-driven over the generated metadata; the
      // waystation-scoped pages are hand-written (see WaystationService).
      resourceRoutes(catalogItemsConfig, resourceMeta),
      resourceRoutes(suppliersConfig, resourceMeta),
      resourceRoutes(staffMembersConfig, resourceMeta),
      resourceRoutes(waystationsConfig, resourceMeta),
      waystationRoute(),
      {
        path: '**',
        redirectTo: 'dashboard',
      },
    ],
  },
];
