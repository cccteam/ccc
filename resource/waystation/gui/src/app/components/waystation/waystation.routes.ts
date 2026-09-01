import { Type } from '@angular/core';
import { Route } from '@angular/router';
import { generatedNavItems } from '@cccteam/ccc-lib/resource-nav';
import { MenuItem } from '@shared/topbar/topbar.component';
import { AuditTrailComponent } from './audit-trail/audit-trail.component';
import { IncidentsComponent } from './incidents/incidents.component';
import { LogisticsComponent } from './logistics/logistics.component';
import { RequisitionsComponent } from './requisitions/requisitions.component';
import { StatusBoardComponent } from './status-board/status-board.component';
import { WorkOrdersComponent } from './work-orders/work-orders.component';

let cachedWaystationRoute: Route | undefined;

/**
 * waystationRoute wires the hand-written station-scoped pages (see WaystationService
 * for why they are hand-written) and registers them in the generated navigation
 * alongside the config-driven resources.
 */
export const waystationRoute = (): Route => {
  if (cachedWaystationRoute) {
    return cachedWaystationRoute;
  }

  const groupItem: MenuItem = { label: 'Station Ops', children: [] };
  generatedNavItems.push(groupItem);
  groupItem.children = [
    { label: 'Work Orders', route: ['station/work-orders'] },
    { label: 'Requisitions', route: ['station/requisitions'] },
    { label: 'Status Board', route: ['station/status-board'] },
    { label: 'Incidents', route: ['station/incidents'] },
    { label: 'Logistics', route: ['station/logistics'] },
    // The audit trail is ring-wide (not station-scoped), but it lives with the
    // hand-written pages: its API surface is the manual-resource route.
    { label: 'Audit Trail', route: ['station/audit-trail'] },
  ];

  cachedWaystationRoute = {
    path: 'station',
    children: [
      {
        path: 'work-orders',
        loadComponent: (): Promise<Type<WorkOrdersComponent>> =>
          import('./work-orders/work-orders.component').then((comp) => comp.WorkOrdersComponent),
      },
      {
        path: 'requisitions',
        loadComponent: (): Promise<Type<RequisitionsComponent>> =>
          import('./requisitions/requisitions.component').then((comp) => comp.RequisitionsComponent),
      },
      {
        path: 'status-board',
        loadComponent: (): Promise<Type<StatusBoardComponent>> =>
          import('./status-board/status-board.component').then((comp) => comp.StatusBoardComponent),
      },
      {
        path: 'incidents',
        loadComponent: (): Promise<Type<IncidentsComponent>> =>
          import('./incidents/incidents.component').then((comp) => comp.IncidentsComponent),
      },
      {
        path: 'logistics',
        loadComponent: (): Promise<Type<LogisticsComponent>> =>
          import('./logistics/logistics.component').then((comp) => comp.LogisticsComponent),
      },
      {
        path: 'audit-trail',
        loadComponent: (): Promise<Type<AuditTrailComponent>> =>
          import('./audit-trail/audit-trail.component').then((comp) => comp.AuditTrailComponent),
      },
    ],
  } satisfies Route;

  return cachedWaystationRoute;
};
