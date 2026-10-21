import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { ResourceRef } from '@angular/core';
import { Component } from '@angular/core';
import { provideRouter, Router } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';
import { TestBed } from '@angular/core/testing';
import { Permissions, Resources } from '@app/service/zz_gen_constants';
import { createApi } from '@app/service/zz_gen_api';
import { ClientBase, PermissionDigest, TransportRequest, TransportResponse } from '@cccteam/resource';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { RESOURCE_CLIENT } from '@cccteam/resource-angular/resource-client';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { environment } from '@env';
import { firstValueFrom } from 'rxjs';
import { grantedColumns, SectorService } from './sector.service';

// A global reader (globalList, globalPage) asks for its list when the digest grants it and
// again only when the answer to that global question changes. The open sector's digest
// landing after sign-in, a reload of the same digests, and a change of sector all leave a
// global grant as it was, so none of them reads the list again; a digest that withholds the
// grant leaves the reader idle at its default, and one that later grants it reads once.

/** The global digest grants the pilot cards when `granted`; every sector's digest grants missions. */
let granted = true;
/** The card fields the global digest enumerates under the grant, when it enumerates any. */
let cardFields: string[] = [];
const sectorDigest: PermissionDigest = { Missions: { List: 'granted', Read: 'granted' } };

/** The global digest: the grant on the cards, with a field-level entry per enumerated field. */
function globalDigest(): PermissionDigest {
  if (!granted) {
    return {};
  }
  const digest: PermissionDigest = { PilotCards: { List: 'granted' } };
  for (const field of cardFields) {
    digest[`PilotCards.${field}`] = { List: 'granted' };
  }
  return digest;
}

/** The server as the spec plays it: the session, the digests, two lit sectors, and the pilot cards. */
function server(request: TransportRequest): TransportResponse {
  const url = new URL(request.url, 'http://lodestar.test');
  switch (url.pathname) {
    case `${environment.apiUrl}/user/session`:
      return { status: 200, body: { authenticated: true, username: 'pilot' } };
    case `${environment.apiUrl}/permission-digest`:
      if (url.searchParams.get('domain')) {
        return { status: 200, body: sectorDigest };
      }
      return { status: 200, body: globalDigest() };
    case `${environment.apiUrl}/user-domains`:
      return { status: 200, body: ['anvil', 'bastion'] };
    case `${environment.apiUrl}/pilot-cards`:
      return { status: 200, body: [{ id: 'p1', callsign: 'Kestrel' }], headers: { 'total-count': '1' } };
    default:
      return { status: 404, body: { message: `unscripted ${request.method} ${request.url}` } };
  }
}

describe('SectorService global readers', () => {
  let transport: ScriptedTransport;

  /** The pilot-cards requests as URLs, in order. */
  const pilotCardRequests = (): URL[] =>
    transport.requests
      .map((r) => new URL(r.url, 'http://lodestar.test'))
      .filter((url) => url.pathname === `${environment.apiUrl}/pilot-cards`);

  /** How many times the pilot cards were asked for, whatever the query. */
  const pilotCardReads = (): number => pilotCardRequests().length;

  /** The columns each pilot-cards request named. */
  const pilotCardColumns = (): (string | null)[] => pilotCardRequests().map((url) => url.searchParams.get('columns'));

  beforeEach(() => {
    cardFields = [];
  });

  /** Runs the root effects and lets pending promises land until `done` answers true, for at most `rounds` rounds. */
  const settle = async (done: () => boolean, rounds = 50): Promise<void> => {
    for (let i = 0; i < rounds && !done(); i++) {
      TestBed.tick();
      await new Promise((resolve) => setTimeout(resolve, 0));
    }
    TestBed.tick();
  };

  /** Signs in over the scripted server (the session, the global digest, the domains) and hands over the service. */
  const signIn = async (): Promise<SectorService> => {
    transport = scriptedTransport(server);
    TestBed.configureTestingModule({
      providers: [
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
        provideHttpClient(),
        provideHttpClientTesting(),
      ],
    });
    await firstValueFrom(TestBed.inject(AuthService).checkUserSession());
    return TestBed.inject(SectorService);
  };

  // The readers are built in an injection context, as a component's field initializer builds them.
  const readers: { name: string; build: (sectors: SectorService) => ResourceRef<unknown> }[] = [
    { name: 'globalList', build: (sectors) => sectors.globalList((api) => api.pilotCards) },
    { name: 'globalPage', build: (sectors) => sectors.globalPage((api) => api.pilotCards) },
  ];

  for (const tt of readers) {
    describe(tt.name, () => {
      it('reads a granted list once: the sector digests landing, a digest reload, and a sector change read nothing more', async () => {
        granted = true;
        const sectors = await signIn();
        const cards = TestBed.runInInjectionContext(() => tt.build(sectors));
        await settle(() => cards.status() === 'resolved');
        expect(pilotCardReads()).toBe(1);

        // Entering a sector loads its digest, as the sector guard's write does; it has
        // landed once the sector's own grant answers.
        await settle(() => sectors.can(Permissions.List, Resources.Missions));
        expect(pilotCardReads()).toBe(1);

        // The same digests again, and another sector entered and its digest landed.
        await TestBed.inject<ClientBase>(RESOURCE_CLIENT).permissions.refresh();
        await settle(() => false, 5);
        sectors.enter('bastion');
        await settle(() => sectors.current() === 'bastion' && sectors.can(Permissions.List, Resources.Missions));
        await settle(() => false, 5);
        expect(pilotCardReads()).toBe(1);
        expect(cards.status()).toBe('resolved');
      });

      it('idles at its default without the grant and reads once when a reloaded digest grants it', async () => {
        granted = false;
        const sectors = await signIn();
        const cards = TestBed.runInInjectionContext(() => tt.build(sectors));
        await settle(() => sectors.can(Permissions.List, Resources.Missions));
        expect(cards.status()).toBe('idle');
        expect(pilotCardReads()).toBe(0);

        granted = true;
        await TestBed.inject<ClientBase>(RESOURCE_CLIENT).permissions.loadDigest();
        await settle(() => cards.status() === 'resolved');
        expect(pilotCardReads()).toBe(1);
      });
    });
  }

  it('answers an empty list from a globalList without the grant', async () => {
    granted = false;
    const sectors = await signIn();
    const cards = TestBed.runInInjectionContext(() => sectors.globalList((api) => api.pilotCards));
    await settle(() => sectors.can(Permissions.List, Resources.Missions));
    expect(cards.value()).toEqual([]);
    expect(pilotCardReads()).toBe(0);
  });

  // A query built from the digest (grantedColumns) names the key and the granted columns;
  // a reload that enumerates the same fields reads nothing more, and one that adds a field
  // (the commendations flag turned on, the digest refreshed by the flip) reads again with it.
  it('a reader with grantedColumns follows the digest: the same fields hold it still, a new field reads again', async () => {
    granted = true;
    cardFields = ['displayName', 'clearance'];
    const sectors = await signIn();
    const cards = TestBed.runInInjectionContext(() => sectors.globalList((api) => api.pilotCards, grantedColumns));
    await settle(() => cards.status() === 'resolved');
    expect(pilotCardColumns()).toEqual(['userId,clearance,displayName']);

    await TestBed.inject<ClientBase>(RESOURCE_CLIENT).permissions.refresh();
    await settle(() => false, 5);
    expect(pilotCardColumns()).toEqual(['userId,clearance,displayName']);

    cardFields = ['displayName', 'clearance', 'commendations'];
    await TestBed.inject<ClientBase>(RESOURCE_CLIENT).permissions.refresh();
    await settle(() => pilotCardReads() === 2);
    expect(pilotCardColumns()).toEqual(['userId,clearance,displayName', 'userId,clearance,commendations,displayName']);
    expect(cards.status()).toBe('resolved');
  });
});

// The star chart's action: open(sector) navigates to the page the browser is on, under the
// chosen sector. A page under a sector keeps its path with the sector replaced, a page
// outside one enters the sector on that path, the refusal page opens the sector's landing,
// and the query travels along. The service writes no sector itself: the guard on the
// segment does, from the new URL.

@Component({ template: 'any page' })
class AnyPageComponent {}

describe('SectorService.open', () => {
  const cases: { name: string; from: string; sector: string; want: string }[] = [
    {
      name: 'a page under a sector keeps its path under the new sector',
      from: '/sectors/anvil/users',
      sector: 'bastion',
      want: '/sectors/bastion/users',
    },
    {
      name: 'the query travels along',
      from: '/sectors/anvil/users?q=1',
      sector: 'bastion',
      want: '/sectors/bastion/users?q=1',
    },
    {
      name: 'a page outside a sector enters the sector on that path',
      from: '/users',
      sector: 'anvil',
      want: '/sectors/anvil/users',
    },
    {
      name: "the dashboard outside a sector opens the sector's dashboard",
      from: '/dashboard',
      sector: 'anvil',
      want: '/sectors/anvil/dashboard',
    },
    {
      name: "the refusal page opens the sector's landing",
      from: '/sectors/zzz/refused',
      sector: 'bastion',
      want: '/sectors/bastion/dashboard',
    },
  ];

  for (const tt of cases) {
    it(tt.name, async () => {
      TestBed.configureTestingModule({
        providers: [
          provideResourceTesting({ client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
          provideRouter([{ path: '**', component: AnyPageComponent }]),
        ],
      });
      const harness = await RouterTestingHarness.create();
      await harness.navigateByUrl(tt.from);
      const sectors = TestBed.inject(SectorService);

      expect(await sectors.open(tt.sector)).toBe(true);

      expect(TestBed.inject(Router).url).toBe(tt.want);
      expect(sectors.current()).toBe('');
    });
  }
});
