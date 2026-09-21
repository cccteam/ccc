import { ComponentFixture, TestBed } from '@angular/core/testing';
import { createApi } from '@app/service/zz_gen_api';
import { ClientBase, Domain, PermissionDigest, TransportRequest, TransportResponse } from '@cccteam/resource';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { RESOURCE_CLIENT } from '@cccteam/resource-angular/resource-client';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { environment } from '@env';
import { firstValueFrom } from 'rxjs';
import { DashboardComponent } from './dashboard.component';

/** What the server answers for one session: its tenants, the global digest, and each tenant's digest. */
interface Grants {
  domains: string[];
  global: PermissionDigest;
  tenants: Record<string, PermissionDigest>;
}

/**
 * A member of the north tenant: Announcements, the one resource this application serves,
 * are north's to list and read, and to update under a condition.
 */
const member: Grants = {
  domains: ['north'],
  global: {},
  tenants: { north: { Announcements: { List: 'granted', Read: 'granted', Update: 'conditional' } } },
};

/** A login that holds nothing anywhere. */
const stranger: Grants = { domains: [], global: {}, tenants: {} };

/**
 * The server as the spec plays it: the session, the digest of the tenant asked for (the
 * global one when none is), and the user's domains. Anything else is a 404 the request log
 * would show.
 */
function server(grants: Grants): (request: TransportRequest) => TransportResponse {
  return (request) => {
    const url = new URL(request.url, 'http://skeleton.test');
    switch (url.pathname) {
      case `${environment.apiUrl}/user/session`:
        return { status: 200, body: { authenticated: true, username: 'member' } };
      case `${environment.apiUrl}/permission-digest`: {
        const domain = url.searchParams.get('domain');
        return { status: 200, body: domain ? (grants.tenants[domain] ?? {}) : grants.global };
      }
      case `${environment.apiUrl}/user-domains`:
        return { status: 200, body: grants.domains };
      default:
        return { status: 404, body: { message: `unscripted ${request.method} ${request.url}` } };
    }
  };
}

describe('DashboardComponent', () => {
  let transport: ScriptedTransport;

  /**
   * Signs in against the scripted server and renders the dashboard. The client is the
   * application's generated one over the scripted transport, so the component reads the
   * digests exactly as it does in the browser: checkUserSession is what a login runs once
   * it is accepted (the session, the global digest, the domains), and the tenant service
   * loads the selected tenant's digest, here loaded ahead so the card renders from it at
   * once.
   */
  async function render(grants: Grants): Promise<ComponentFixture<DashboardComponent>> {
    transport = scriptedTransport(server(grants));
    await TestBed.configureTestingModule({
      imports: [DashboardComponent],
      providers: [
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
      ],
    }).compileComponents();
    await firstValueFrom(TestBed.inject(AuthService).checkUserSession());
    for (const domain of grants.domains) {
      await TestBed.inject<ClientBase>(RESOURCE_CLIENT).permissions.loadDigest(domain as Domain);
    }
    const fixture = TestBed.createComponent(DashboardComponent);
    fixture.detectChanges();
    return fixture;
  }

  /** The state class of each cell in a row: the columns are List, Read, Create, Update, Delete, Execute. */
  function states(row: Element | undefined): (string | undefined)[] {
    return Array.from(row?.querySelectorAll('td.state') ?? []).map((cell) =>
      Array.from(cell.classList).find((name) => name !== 'state'),
    );
  }

  it('renders who is signed in and the digest per resource, tenant-scoped ones in the selected tenant', async () => {
    const fixture = await render(member);
    const element: HTMLElement = fixture.nativeElement;

    expect(element.querySelector('h1')?.textContent).toContain('Signed in as member');
    expect(element.querySelector('p strong')?.textContent).toBe('north');
    const rows = Array.from(element.querySelectorAll('tbody tr'));
    const labels = rows.map((row) => row.querySelector('td')?.textContent?.replace(/\s+/g, ' ').trim());
    expect(labels).toEqual(['Announcements north']);
    expect(states(rows[0])).toEqual(['granted', 'granted', 'absent', 'conditional', 'absent', 'absent']);

    const asked = transport.requests.map((request) => `${request.method} ${request.url}`);
    expect(asked.slice(0, 4)).toEqual([
      `GET ${environment.apiUrl}/user/session`,
      `GET ${environment.apiUrl}/permission-digest`,
      `GET ${environment.apiUrl}/user-domains`,
      `GET ${environment.apiUrl}/permission-digest?domain=north`,
    ]);
  });

  it('says so when the session holds nothing', async () => {
    const fixture = await render(stranger);
    const element: HTMLElement = fixture.nativeElement;

    expect(element.querySelector('p strong')?.textContent).toBe('no tenant');
    expect(element.querySelectorAll('tbody tr')).toHaveLength(1);
    expect(element.querySelector('td.empty')?.textContent).toContain('You hold nothing here');
  });
});
