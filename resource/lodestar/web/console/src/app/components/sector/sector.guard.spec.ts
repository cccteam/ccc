import { Component } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter, Routes, withComponentInputBinding, withRouterConfig } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';
import { createApi } from '@app/service/zz_gen_api';
import { DomainRoutePath } from '@app/service/zz_gen_resources';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { TransportRequest, TransportResponse } from '@cccteam/resource';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { environment } from '@env';
import { firstValueFrom } from 'rxjs';
import { sectorGuard, sectorLeave } from './sector.guard';
import { refusedSegment, SectorService } from './sector.service';

// The sector guard on the sector segment is the one writer of the sector the decks read:
// a sector the session holds opens, with the sector written once before the page loads; a
// sector the session cannot reach is refused onto the refusal page under its own URL, and
// nothing is written; on a deep link before the session's sector list has landed the guard
// asks for the list first; leaving the segment clears the sector and moving to another
// sector writes the new one.
//
// Demonstrates: tenancy.concealed, tenancy.user-domains.

@Component({ template: 'the page' })
class PageComponent {}

@Component({ template: 'refused' })
class RefusedComponent {}

@Component({ template: 'elsewhere' })
class ElsewhereComponent {}

const testRoutes: Routes = [
  { path: 'elsewhere', component: ElsewhereComponent },
  { path: `${DomainRoutePath}/${refusedSegment}`, component: RefusedComponent },
  {
    path: DomainRoutePath,
    canActivate: [sectorGuard],
    canDeactivate: [sectorLeave],
    children: [{ path: 'page', component: PageComponent }],
  },
];

/** The server as the spec plays it: the session, the digests, and the session's sectors. */
function serverFor(sectors: string[]): (request: TransportRequest) => TransportResponse {
  return (request) => {
    const url = new URL(request.url, 'http://lodestar.test');
    switch (url.pathname) {
      case `${environment.apiUrl}/user/session`:
        return { status: 200, body: { authenticated: true, username: 'member' } };
      case `${environment.apiUrl}/permission-digest`:
        return { status: 200, body: {} };
      case `${environment.apiUrl}/user-domains`:
        return { status: 200, body: sectors };
      case `${environment.apiUrl}/features`:
        return { status: 200, body: { enabled: [] } };
      default:
        return { status: 404, body: { message: `unscripted ${request.method} ${request.url}` } };
    }
  };
}

describe('sectorGuard', () => {
  let transport: ScriptedTransport;

  const listLoads = (): number => transport.requests.filter((r) => r.url.endsWith('/user-domains')).length;

  /** Configures the app over a scripted server; with `preload`, the session's sector list has landed. */
  const appOver = async (sectors: string[], preload: boolean): Promise<void> => {
    transport = scriptedTransport(serverFor(sectors));
    TestBed.configureTestingModule({
      providers: [
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
        provideRouter(
          testRoutes,
          withComponentInputBinding(),
          withRouterConfig({ paramsInheritanceStrategy: 'always' }),
        ),
      ],
    });
    if (preload) {
      await firstValueFrom(TestBed.inject(AuthService).loadDomains());
    }
  };

  const cases: {
    name: string;
    sectors: string[];
    preload: boolean;
    navigate: string[];
    want: 'page' | 'refused' | 'elsewhere';
    wantSector: string;
    wantEntered: string[];
    wantLeft: number;
    wantListLoads: number;
  }[] = [
    {
      name: 'a sector the session holds opens, written once before the page loads',
      sectors: ['anvil'],
      preload: true,
      navigate: ['/sectors/anvil/page'],
      want: 'page',
      wantSector: 'anvil',
      wantEntered: ['anvil'],
      wantLeft: 0,
      wantListLoads: 1,
    },
    {
      name: 'a sector the session cannot reach is refused under its own URL, and nothing is written',
      sectors: ['anvil'],
      preload: true,
      navigate: ['/sectors/zzz/page'],
      want: 'refused',
      wantSector: '',
      wantEntered: [],
      wantLeft: 0,
      wantListLoads: 2,
    },
    {
      name: 'a deep link before the sector list has landed asks for the list first, then opens',
      sectors: ['anvil'],
      preload: false,
      navigate: ['/sectors/anvil/page'],
      want: 'page',
      wantSector: 'anvil',
      wantEntered: ['anvil'],
      wantLeft: 0,
      wantListLoads: 1,
    },
    {
      name: 'leaving the sector segment clears the sector',
      sectors: ['anvil'],
      preload: true,
      navigate: ['/sectors/anvil/page', '/elsewhere'],
      want: 'elsewhere',
      wantSector: '',
      wantEntered: ['anvil'],
      wantLeft: 1,
      wantListLoads: 1,
    },
    {
      name: 'moving to another sector writes the new one, and clears nothing in between',
      sectors: ['anvil', 'bastion'],
      preload: true,
      navigate: ['/sectors/anvil/page', '/sectors/bastion/page'],
      want: 'page',
      wantSector: 'bastion',
      wantEntered: ['anvil', 'bastion'],
      wantLeft: 0,
      wantListLoads: 1,
    },
  ];

  for (const tt of cases) {
    it(tt.name, async () => {
      await appOver(tt.sectors, tt.preload);
      const sectors = TestBed.inject(SectorService);
      const entered = vi.spyOn(sectors, 'enter');
      const left = vi.spyOn(sectors, 'leave');
      const harness = await RouterTestingHarness.create();

      let activated: unknown;
      for (const url of tt.navigate) {
        activated = await harness.navigateByUrl(url);
      }

      const components = { page: PageComponent, refused: RefusedComponent, elsewhere: ElsewhereComponent };
      expect(activated).toBeInstanceOf(components[tt.want]);
      expect(sectors.current()).toBe(tt.wantSector);
      expect(entered.mock.calls.map(([sector]) => sector)).toEqual(tt.wantEntered);
      expect(left).toHaveBeenCalledTimes(tt.wantLeft);
      expect(listLoads()).toBe(tt.wantListLoads);
    });
  }
});
