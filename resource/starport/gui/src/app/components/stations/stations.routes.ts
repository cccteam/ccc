import { Type } from '@angular/core';
import { Route } from '@angular/router';
import { generatedNavItems } from '@cccteam/ccc-lib/resource-nav';
import { MenuItem } from '@shared/topbar/topbar.component';
import { AuthorizeDockingComponent } from './authorize-docking/authorize-docking.component';
import { BerthsComponent } from './berths/berths.component';

let cachedStationsRoute: Route | undefined;

/**
 * stationsRoute wires the hand-written station-scoped pages (see StationsService for
 * why they are hand-written) and registers them in the generated navigation alongside
 * the config-driven resources.
 */
export const stationsRoute = (): Route => {
  if (cachedStationsRoute) {
    return cachedStationsRoute;
  }

  const groupItem: MenuItem = { label: 'Stations', children: [] };
  generatedNavItems.push(groupItem);
  groupItem.children = [
    { label: 'Berths', route: ['stations/berths'] },
    { label: 'Authorize Docking', route: ['stations/authorize-docking'] },
  ];

  cachedStationsRoute = {
    path: 'stations',
    children: [
      {
        path: 'berths',
        loadComponent: (): Promise<Type<BerthsComponent>> =>
          import('./berths/berths.component').then((comp) => comp.BerthsComponent),
      },
      {
        path: 'authorize-docking',
        loadComponent: (): Promise<Type<AuthorizeDockingComponent>> =>
          import('./authorize-docking/authorize-docking.component').then((comp) => comp.AuthorizeDockingComponent),
      },
    ],
  } satisfies Route;

  return cachedStationsRoute;
};
