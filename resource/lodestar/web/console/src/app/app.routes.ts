import { Routes } from '@angular/router';
import { resourceMeta } from '@app/service/zz_gen_resources';
import { LoginAuthenticationGuard } from '@cccteam/ccc-lib/auth-authentication-guard';
import { resourceRoutes } from '@cccteam/ccc-lib/resource-route-generator';
import { sectorRoute } from '@components/sector/sector.routes';
import { UiComponent } from '@components/ui/ui.component';
import { clientsConfig } from './configs/clients.config';
import { pilotsConfig } from './configs/pilots.config';
import { sectorsConfig } from './configs/sectors.config';
import { shipClassesConfig } from './configs/shipClasses.config';

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
      // sector-scoped decks are hand-written (see SectorService).
      resourceRoutes(clientsConfig, resourceMeta),
      resourceRoutes(shipClassesConfig, resourceMeta),
      resourceRoutes(pilotsConfig, resourceMeta),
      resourceRoutes(sectorsConfig, resourceMeta),
      sectorRoute(),
      { path: '**', redirectTo: 'dashboard' },
    ],
  },
];
