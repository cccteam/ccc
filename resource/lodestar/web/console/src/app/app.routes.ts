import { Routes } from '@angular/router';
import { resourceMeta } from '@app/service/zz_gen_resources';
import { LoginAuthenticationGuard } from '@cccteam/resource-angular/auth-authentication-guard';
import { resourceRoutes } from '@cccteam/resource-angular/resource-route-generator';
import { sectorRoute } from '@components/sector/sector.routes';
import { UiComponent } from '@components/ui/ui.component';
import { clientsConfig } from './configs/clients.config';
import { distressCallsConfig } from './configs/distressCalls.config';
import { missionDocumentsConfig } from './configs/missionDocuments.config';
import { missionsConfig } from './configs/missions.config';
import { squadronsConfig } from './configs/squadrons.config';
import { pilotsConfig } from './configs/pilots.config';
import { sectorsConfig } from './configs/sectors.config';
import { shipClassesConfig } from './configs/shipClasses.config';
import { shipsConfig } from './configs/ships.config';
import { standingOrdersConfig } from './configs/standingOrders.config';

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
      // sector-scoped decks are hand-written (see SectorService), except the Missions
      // page, a config-driven page in the selected sector (RESOURCE_DOMAIN) whose
      // pickers list exactly the resources the metadata names.
      //
      // Every config-driven list and row route carries the library's canDeactivateGuard,
      // attached by resourceRoutes: leaving a page whose form is dirty (FormStateService)
      // opens the library's LeavePageConfirmationModalComponent, and a route to the
      // FRONTEND_LOGIN_PATH app.config.ts provides leaves without asking. The console
      // adds no guard of its own.
      //
      // Demonstrates: form.leave-page.
      resourceRoutes(clientsConfig, resourceMeta),
      resourceRoutes(shipClassesConfig, resourceMeta),
      // The standing orders are a key-less list: resourceRoutes builds the list route
      // alone, no `:uuid` row route, since a line has no key to open by.
      resourceRoutes(standingOrdersConfig, resourceMeta),
      resourceRoutes(pilotsConfig, resourceMeta),
      resourceRoutes(sectorsConfig, resourceMeta),
      resourceRoutes(missionsConfig, resourceMeta),
      resourceRoutes(shipsConfig, resourceMeta),
      resourceRoutes(squadronsConfig, resourceMeta),
      // The Documents and Calls pages carry the field shapes a form never types (bytes,
      // an object) and the write-only transcript, beside the hand-written call log.
      resourceRoutes(missionDocumentsConfig, resourceMeta),
      resourceRoutes(distressCallsConfig, resourceMeta),
      sectorRoute(),
      { path: '**', redirectTo: 'dashboard' },
    ],
  },
];
