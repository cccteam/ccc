import { Type } from '@angular/core';
import { Route } from '@angular/router';
import { Methods, Permissions, Resources } from '@app/service/zz_gen_constants';
import { generatedNavItems } from '@cccteam/resource-angular/resource-nav';
import { MenuItem } from '@shared/topbar/topbar.component';
import { BriefingComponent } from './briefing/briefing.component';
import { CallLogComponent } from './call-log/call-log.component';
import { FlightDeckComponent } from './flight-deck/flight-deck.component';
import { HangarDeckComponent } from './hangar-deck/hangar-deck.component';
import { HazardBoardComponent } from './hazard-board/hazard-board.component';
import { RosterComponent } from './roster/roster.component';
import { SalvageHoldComponent } from './salvage-hold/salvage-hold.component';
import { ShipsLogComponent } from './ships-log/ships-log.component';
import { WatchDeskComponent } from './watch-desk/watch-desk.component';

let cachedSectorRoute: Route | undefined;

/**
 * sectorRoute wires the hand-written sector-scoped decks (see SectorService for why
 * they are hand-written) and registers them in the generated navigation alongside the
 * config-driven global resources. Each item names the List grant its deck needs; the
 * topbar asks the digest for the selected sector.
 */
export const sectorRoute = (): Route => {
  if (cachedSectorRoute) {
    return cachedSectorRoute;
  }

  const groupItem: MenuItem = { label: 'Sector Ops', children: [] };
  generatedNavItems.push(groupItem);
  const list = Permissions.List;
  groupItem.children = [
    { label: 'Flight Deck', route: ['sector/flight-deck'], permission: { resource: Resources.Missions, permission: list } },
    { label: 'Hangar Deck', route: ['sector/hangar-deck'], permission: { resource: Resources.Refits, permission: list } },
    { label: 'Salvage Hold', route: ['sector/salvage-hold'], permission: { resource: Resources.Consignments, permission: list } },
    { label: 'Call Log', route: ['sector/call-log'], permission: { resource: Resources.DistressCalls, permission: list } },
    { label: 'Squadron Roster', route: ['sector/roster'], permission: { resource: Resources.Squadrons, permission: list } },
    { label: 'Hazard Board', route: ['sector/hazard-board'], permission: { resource: Resources.SectorHazardBoards, permission: list } },
    { label: "Ship's Log", route: ['sector/ships-log'], permission: { resource: Resources.ShipsLogEntries, permission: list } },
    { label: 'Briefing', route: ['sector/briefing'], permission: { resource: Methods.CompileBriefing, permission: Permissions.Execute } },
    { label: 'Watch Desk', route: ['sector/watch-desk'], permission: { resource: Methods.ViewAsUser, permission: Permissions.Execute } },
  ];

  cachedSectorRoute = {
    path: 'sector',
    children: [
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
        path: 'watch-desk',
        loadComponent: (): Promise<Type<WatchDeskComponent>> =>
          import('./watch-desk/watch-desk.component').then((comp) => comp.WatchDeskComponent),
      },
    ],
  } satisfies Route;

  return cachedSectorRoute;
};
