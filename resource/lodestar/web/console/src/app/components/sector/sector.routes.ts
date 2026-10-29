import { Type } from '@angular/core';
import { Route } from '@angular/router';
import { Methods, Permissions, Resources } from '@app/service/zz_gen_constants';
import { DomainRoutePath } from '@app/service/zz_gen_resources';
import { generatedNavItems } from '@cccteam/resource-angular/resource-nav';
import { DashboardComponent } from '@components/ui/dashboard/dashboard.component';
import { BriefingComponent } from './briefing/briefing.component';
import { CallLogComponent } from './call-log/call-log.component';
import { FlightDeckComponent } from './flight-deck/flight-deck.component';
import { HangarDeckComponent } from './hangar-deck/hangar-deck.component';
import { HazardBoardComponent } from './hazard-board/hazard-board.component';
import { RosterComponent } from './roster/roster.component';
import { SalvageHoldComponent } from './salvage-hold/salvage-hold.component';
import { sectorGuard, sectorLeave } from './sector.guard';
import { SectorSignInComponent } from './sector-sign-in/sector-sign-in.component';
import { SectorUsersComponent } from './sector-users/sector-users.component';
import { ShipsLogComponent } from './ships-log/ships-log.component';
import { WatchDeskComponent } from './watch-desk/watch-desk.component';

let cachedSectorRoute: Route | undefined;

/**
 * sectorRoute is the sector segment, `sectors/:sectorID` (the generated DomainRoutePath,
 * mirroring the API's): every sector-scoped page lives under it, so a link to one can be
 * shared and a bookmark lands where it points. The guard settles the sector from the URL
 * before any page here loads and clears it on the way out; nothing else writes it. Under
 * it: the dashboard in the sector, the hand-written decks (see SectorService for why they
 * are hand-written), the sector's crew page, the sector's sign-in page (who may enter it:
 * its declared directories and whether they are the only way in), and the config-driven pages the caller hands
 * in, which resourceRoutes registered at the route their configs name
 * (`sectors/:sectorID/missions`) in the navigation and as where their rows open, and
 * which are mounted here by that route's last part. The decks join the Sector Ops group
 * those config pages named in the generated navigation (found by its label, made here
 * only when no config page named it, so the top bar lists the group once): each item
 * names the segment's route parameter, which the top bar fills with the open sector, and
 * the List grant its deck needs, asked of that sector's digest. The watch desk is global
 * (every live impersonation in the service) and lives outside the segment, listed with
 * the decks.
 */
export const sectorRoute = (pages: Route[]): Route => {
  if (cachedSectorRoute) {
    return cachedSectorRoute;
  }

  let groupItem = generatedNavItems.find((item) => item.label === 'Sector Ops');
  if (!groupItem) {
    groupItem = { label: 'Sector Ops', children: [] };
    generatedNavItems.push(groupItem);
  }
  const list = Permissions.List;
  groupItem.children = [
    ...(groupItem.children ?? []),
    {
      label: 'Flight Deck',
      route: [DomainRoutePath, 'flight-deck'],
      permission: { resource: Resources.Missions, permission: list },
    },
    {
      label: 'Hangar Deck',
      route: [DomainRoutePath, 'hangar-deck'],
      permission: { resource: Resources.Refits, permission: list },
    },
    {
      label: 'Salvage Hold',
      route: [DomainRoutePath, 'salvage-hold'],
      permission: { resource: Resources.Consignments, permission: list },
    },
    {
      label: 'Call Log',
      route: [DomainRoutePath, 'call-log'],
      permission: { resource: Resources.DistressCalls, permission: list },
    },
    {
      label: 'Squadron Roster',
      route: [DomainRoutePath, 'roster'],
      permission: { resource: Resources.Squadrons, permission: list },
    },
    {
      label: 'Hazard Board',
      route: [DomainRoutePath, 'hazard-board'],
      permission: { resource: Resources.SectorHazardBoards, permission: list },
    },
    {
      label: "Ship's Log",
      route: [DomainRoutePath, 'ships-log'],
      permission: { resource: Resources.ShipsLogEntries, permission: list },
    },
    {
      label: 'Briefing',
      route: [DomainRoutePath, 'briefing'],
      permission: { resource: Methods.CompileBriefing, permission: Permissions.Execute },
    },
    {
      label: 'Sector Crew',
      route: [DomainRoutePath, 'users'],
      permission: { resource: Resources.CrewSectorUsers, permission: list },
    },
    {
      label: 'Sign-in',
      route: [DomainRoutePath, 'sign-in'],
      permission: { resource: Resources.SectorDirectories, permission: list },
    },
    {
      label: 'Watch Desk',
      route: ['watch-desk'],
      permission: { resource: Methods.ViewAsUser, permission: Permissions.Execute },
    },
  ];

  // A config-driven page, mounted under the segment by the last part of the route its
  // config names.
  const inSector = (page: Route): Route => ({ ...page, path: page.path?.slice(DomainRoutePath.length + 1) });

  cachedSectorRoute = {
    path: DomainRoutePath,
    canActivate: [sectorGuard],
    canDeactivate: [sectorLeave],
    children: [
      {
        path: 'dashboard',
        loadComponent: (): Promise<Type<DashboardComponent>> =>
          import('@components/ui/dashboard/dashboard.component').then((comp) => comp.DashboardComponent),
      },
      ...pages.map(inSector),
      {
        path: 'flight-deck',
        loadComponent: (): Promise<Type<FlightDeckComponent>> =>
          import('./flight-deck/flight-deck.component').then((comp) => comp.FlightDeckComponent),
      },
      {
        path: 'hangar-deck',
        loadComponent: (): Promise<Type<HangarDeckComponent>> =>
          import('./hangar-deck/hangar-deck.component').then((comp) => comp.HangarDeckComponent),
      },
      {
        path: 'salvage-hold',
        loadComponent: (): Promise<Type<SalvageHoldComponent>> =>
          import('./salvage-hold/salvage-hold.component').then((comp) => comp.SalvageHoldComponent),
      },
      {
        path: 'call-log',
        loadComponent: (): Promise<Type<CallLogComponent>> =>
          import('./call-log/call-log.component').then((comp) => comp.CallLogComponent),
      },
      {
        path: 'roster',
        loadComponent: (): Promise<Type<RosterComponent>> =>
          import('./roster/roster.component').then((comp) => comp.RosterComponent),
      },
      {
        path: 'hazard-board',
        loadComponent: (): Promise<Type<HazardBoardComponent>> =>
          import('./hazard-board/hazard-board.component').then((comp) => comp.HazardBoardComponent),
      },
      {
        path: 'ships-log',
        loadComponent: (): Promise<Type<ShipsLogComponent>> =>
          import('./ships-log/ships-log.component').then((comp) => comp.ShipsLogComponent),
      },
      {
        path: 'briefing',
        loadComponent: (): Promise<Type<BriefingComponent>> =>
          import('./briefing/briefing.component').then((comp) => comp.BriefingComponent),
      },
      {
        path: 'users',
        loadComponent: (): Promise<Type<SectorUsersComponent>> =>
          import('./sector-users/sector-users.component').then((comp) => comp.SectorUsersComponent),
      },
      // Who may enter the sector: its directories and whether they are the only way in.
      {
        path: 'sign-in',
        loadComponent: (): Promise<Type<SectorSignInComponent>> =>
          import('./sector-sign-in/sector-sign-in.component').then((comp) => comp.SectorSignInComponent),
      },
      { path: '**', redirectTo: 'dashboard' },
    ],
  } satisfies Route;

  return cachedSectorRoute;
};

/** watchDeskRoute is the watch desk, a global page listed with the sector decks. */
export const watchDeskRoute = (): Route => ({
  path: 'watch-desk',
  loadComponent: (): Promise<Type<WatchDeskComponent>> =>
    import('./watch-desk/watch-desk.component').then((comp) => comp.WatchDeskComponent),
});
