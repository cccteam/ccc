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
import { tenantGuard, tenantLeave } from './tenant.guard';
import { refusedSegment, TenantService } from './tenant.service';

// The tenant guard on the tenant segment is the one writer of the tenant the pages read:
// a tenant the session holds opens, with the tenant written once before the page loads; a
// tenant the session cannot reach is refused onto the refusal page under its own URL, and
// nothing is written; on a deep link before the session's tenant list has landed the guard
// asks for the list first; leaving the segment clears the tenant and moving to another
// tenant writes the new one.

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
    canActivate: [tenantGuard],
    canDeactivate: [tenantLeave],
    children: [{ path: 'page', component: PageComponent }],
  },
];

/** The server as the spec plays it: the session, the digests, and the session's tenants. */
function serverFor(tenants: string[]): (request: TransportRequest) => TransportResponse {
  return (request) => {
    const url = new URL(request.url, 'http://skeleton.test');
    switch (url.pathname) {
      case `${environment.apiUrl}/user/session`:
        return { status: 200, body: { authenticated: true, username: 'member' } };
      case `${environment.apiUrl}/permission-digest`:
        return { status: 200, body: {} };
      case `${environment.apiUrl}/user-domains`:
        return { status: 200, body: tenants };
      case `${environment.apiUrl}/features`:
        return { status: 200, body: { enabled: [] } };
      default:
        return { status: 404, body: { message: `unscripted ${request.method} ${request.url}` } };
    }
  };
}

describe('tenantGuard', () => {
  let transport: ScriptedTransport;

  const listLoads = (): number => transport.requests.filter((r) => r.url.endsWith('/user-domains')).length;

  /** Configures the app over a scripted server; with `preload`, the session's tenant list has landed. */
  const appOver = async (tenants: string[], preload: boolean): Promise<void> => {
    transport = scriptedTransport(serverFor(tenants));
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
    tenants: string[];
    preload: boolean;
    navigate: string[];
    want: 'page' | 'refused' | 'elsewhere';
    wantTenant: string;
    wantEntered: string[];
    wantLeft: number;
    wantListLoads: number;
  }[] = [
    {
      name: 'a tenant the session holds opens, written once before the page loads',
      tenants: ['acme'],
      preload: true,
      navigate: ['/tenants/acme/page'],
      want: 'page',
      wantTenant: 'acme',
      wantEntered: ['acme'],
      wantLeft: 0,
      wantListLoads: 1,
    },
    {
      name: 'a tenant the session cannot reach is refused under its own URL, and nothing is written',
      tenants: ['acme'],
      preload: true,
      navigate: ['/tenants/zzz/page'],
      want: 'refused',
      wantTenant: '',
      wantEntered: [],
      wantLeft: 0,
      wantListLoads: 2,
    },
    {
      name: 'a deep link before the tenant list has landed asks for the list first, then opens',
      tenants: ['acme'],
      preload: false,
      navigate: ['/tenants/acme/page'],
      want: 'page',
      wantTenant: 'acme',
      wantEntered: ['acme'],
      wantLeft: 0,
      wantListLoads: 1,
    },
    {
      name: 'leaving the tenant segment clears the tenant',
      tenants: ['acme'],
      preload: true,
      navigate: ['/tenants/acme/page', '/elsewhere'],
      want: 'elsewhere',
      wantTenant: '',
      wantEntered: ['acme'],
      wantLeft: 1,
      wantListLoads: 1,
    },
    {
      name: 'moving to another tenant writes the new one, and clears nothing in between',
      tenants: ['acme', 'beta'],
      preload: true,
      navigate: ['/tenants/acme/page', '/tenants/beta/page'],
      want: 'page',
      wantTenant: 'beta',
      wantEntered: ['acme', 'beta'],
      wantLeft: 0,
      wantListLoads: 1,
    },
  ];

  for (const tt of cases) {
    it(tt.name, async () => {
      await appOver(tt.tenants, tt.preload);
      const tenants = TestBed.inject(TenantService);
      const entered = vi.spyOn(tenants, 'enter');
      const left = vi.spyOn(tenants, 'leave');
      const harness = await RouterTestingHarness.create();

      let activated: unknown;
      for (const url of tt.navigate) {
        activated = await harness.navigateByUrl(url);
      }

      const components = { page: PageComponent, refused: RefusedComponent, elsewhere: ElsewhereComponent };
      expect(activated).toBeInstanceOf(components[tt.want]);
      expect(tenants.current()).toBe(tt.wantTenant);
      expect(entered.mock.calls.map(([tenant]) => tenant)).toEqual(tt.wantEntered);
      expect(left).toHaveBeenCalledTimes(tt.wantLeft);
      expect(listLoads()).toBe(tt.wantListLoads);
    });
  }
});
