import { Routes } from '@angular/router';
import { resourceMeta } from '@app/service/zz_gen_resources';
import { LoginAuthenticationGuard } from '@cccteam/ccc-lib/auth-authentication-guard';
import { resourceRoutes } from '@cccteam/ccc-lib/resource-route-generator';
import { stationsRoute } from '@components/stations/stations.routes';
import { UiComponent } from '@components/ui/ui.component';
import { cargoManifestsConfig } from './configs/cargoManifests.config';
import { crewMembersConfig } from './configs/crewMembers.config';
import { dockingBaysConfig } from './configs/dockingBays.config';
import { manifestLinesConfig } from './configs/manifestLines.config';
import { shipCargoSummariesConfig } from './configs/shipCargoSummaries.config';
import { shipsConfig } from './configs/ships.config';
import { supplyCratesConfig } from './configs/supplyCrates.config';

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
      resourceRoutes(shipsConfig, resourceMeta),
      resourceRoutes(dockingBaysConfig, resourceMeta),
      resourceRoutes(crewMembersConfig, resourceMeta),
      resourceRoutes(cargoManifestsConfig, resourceMeta),
      resourceRoutes(supplyCratesConfig, resourceMeta),
      resourceRoutes(shipCargoSummariesConfig, resourceMeta),
      resourceRoutes(manifestLinesConfig, resourceMeta),
      stationsRoute(),
      {
        path: '**',
        redirectTo: 'dashboard',
      },
    ],
  },
];
