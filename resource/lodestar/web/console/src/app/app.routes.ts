import { Routes } from '@angular/router';
import { DomainRoutePath, resourceMeta } from '@app/service/zz_gen_resources';
import { LoginAuthenticationGuard } from '@cccteam/resource-angular/auth-authentication-guard';
import { resourceRoutes } from '@cccteam/resource-angular/resource-route-generator';
import { sectorRoute, watchDeskRoute } from '@components/sector/sector.routes';
import { refusedSegment } from '@components/sector/sector.service';
import { UiComponent } from '@components/ui/ui.component';
import { usersRoute } from '@components/ui/users/users.routes';
import { clientsConfig } from './configs/clients.config';
import { commendationsConfig } from './configs/commendations.config';
import { distressCallsConfig } from './configs/distressCalls.config';
import { missionDocumentsConfig } from './configs/missionDocuments.config';
import { missionsConfig } from './configs/missions.config';
import { squadronsConfig } from './configs/squadrons.config';
import { pilotsConfig, sectorPilotsConfig } from './configs/pilots.config';
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
    path: 'logout',
    loadComponent: () => import('@cccteam/resource-angular/auth-sign-out').then((comp) => comp.SignOutComponent),
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
      // page and its siblings under the sector segment below, config-driven pages in the
      // sector the URL names (RESOURCE_DOMAIN) whose pickers list exactly the resources
      // the metadata names.
      //
      // Every config-driven list and row route carries the library's canDeactivateGuard,
      // attached by resourceRoutes: leaving a page whose form is dirty (FormStateService)
      // opens the library's LeavePageConfirmationModalComponent, and a route to the
      // FRONTEND_LOGIN_PATH app.config.ts provides leaves without asking. The console
      // adds no leave-page guard of its own.
      //
      // Demonstrates: form.leave-page.
      resourceRoutes(clientsConfig, resourceMeta),
      resourceRoutes(shipClassesConfig, resourceMeta),
      // The standing orders are a key-less list: resourceRoutes builds the list route
      // alone, no `:uuid` row route, since a line has no key to open by.
      resourceRoutes(standingOrdersConfig, resourceMeta),
      resourceRoutes(pilotsConfig, resourceMeta),
      // The commendations desk sits behind the one feature flag. Nothing here says so:
      // resourceRoutes reads `feature` from the generated metadata and matches the route
      // only while the flag is on (the library's featureMatch guard), so a URL to the
      // desk while it is off falls to the wildcard below as an unknown URL does.
      //
      // Demonstrates: @feature.
      resourceRoutes(commendationsConfig, resourceMeta),
      resourceRoutes(sectorsConfig, resourceMeta),
      // The Users page composes the library's user-management component over the global
      // partitions of the surfaces the console serves; it joins the Headquarters group the
      // config-driven pages above registered. The sector's crew is the sector's own page.
      usersRoute(),
      // The watch desk lists every live impersonation in the service: global, listed with
      // the sector decks.
      watchDeskRoute(),
      // Where a URL naming a sector the session cannot reach lands (sectorGuard): the
      // server's answer, under the sector's own URL. Declared ahead of the segment, so the
      // guard is not asked again on the way there.
      {
        path: `${DomainRoutePath}/${refusedSegment}`,
        loadComponent: () =>
          import('./components/sector/sector-refused/sector-refused.component').then(
            (comp) => comp.SectorRefusedComponent,
          ),
      },
      // The sector segment, sectors/:sectorID, mirroring the API's: every sector-scoped
      // page lives under it. The config-driven pages in the sector are registered by
      // resourceRoutes at the route their configs name, sectors/:sectorID/<page>, and
      // mounted under the segment with the decks; the Documents and Calls pages among
      // them carry the field shapes a form never types (bytes, an object) and the
      // write-only transcript, beside the hand-written call log, and the Pilots page is
      // served here a second time, where a pilot's assignments in the sector show.
      sectorRoute([
        resourceRoutes(sectorPilotsConfig, resourceMeta),
        resourceRoutes(missionsConfig, resourceMeta),
        resourceRoutes(shipsConfig, resourceMeta),
        resourceRoutes(squadronsConfig, resourceMeta),
        resourceRoutes(missionDocumentsConfig, resourceMeta),
        resourceRoutes(distressCallsConfig, resourceMeta),
      ]),
      { path: '**', redirectTo: 'dashboard' },
    ],
  },
];
