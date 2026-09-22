import { ComponentFixture, TestBed } from '@angular/core/testing';
import { createApi } from '@app/service/zz_gen_api';
import { PermissionDigest, TransportRequest, TransportResponse } from '@cccteam/resource';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { environment } from '@env';
import { firstValueFrom } from 'rxjs';
import { DashboardComponent } from './dashboard.component';

/**
 * The digest the server answers for a session that lists and reads Beacons, updates them
 * under a condition, and lists Harbors. The dotted entry is a field grant; the dashboard
 * folds it into its resource's row.
 */
const digest: PermissionDigest = {
  Beacons: { List: 'granted', Read: 'granted', Update: 'conditional' },
  'Beacons.Name': { Read: 'granted' },
  Harbors: { List: 'granted' },
};

/**
 * The server as the spec plays it: the three routes a sign-in touches, answered from the
 * test. Anything else is a 404 the assertions on the request log would show.
 */
function server(answer: PermissionDigest): (request: TransportRequest) => TransportResponse {
  return (request) => {
    switch (request.url) {
      case `${environment.apiUrl}/user/session`:
        return { status: 200, body: { authenticated: true, username: 'admin' } };
      case `${environment.apiUrl}/permission-digest`:
        return { status: 200, body: answer };
      case `${environment.apiUrl}/user-domains`:
        return { status: 200, body: [] };
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
   * digest exactly as it does in the browser; checkUserSession is what the login page runs
   * once the credentials are accepted, and it loads the digest and the domains.
   */
  async function render(answer: PermissionDigest): Promise<ComponentFixture<DashboardComponent>> {
    transport = scriptedTransport(server(answer));
    await TestBed.configureTestingModule({
      imports: [DashboardComponent],
      providers: [
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
      ],
    }).compileComponents();
    await firstValueFrom(TestBed.inject(AuthService).checkUserSession());
    const fixture = TestBed.createComponent(DashboardComponent);
    fixture.detectChanges();
    return fixture;
  }

  it('renders who is signed in and the digest per resource, field entries folded into their row', async () => {
    const fixture = await render(digest);
    const element: HTMLElement = fixture.nativeElement;

    expect(element.querySelector('h1')?.textContent).toContain('Signed in as admin');
    const rows = Array.from(element.querySelectorAll('tbody tr'));
    expect(rows.map((row) => row.querySelector('td')?.textContent?.trim())).toEqual(['Beacons', 'Harbors']);
    // The columns are List, Read, Create, Update, Delete, Execute; each cell carries its state as a class.
    const states = Array.from(rows[0]?.querySelectorAll('td.state') ?? []).map((cell) =>
      Array.from(cell.classList).find((name) => name !== 'state'),
    );
    expect(states).toEqual(['granted', 'granted', 'absent', 'conditional', 'absent', 'absent']);
    expect(transport.requests.map((request) => `${request.method} ${request.url}`)).toEqual([
      `GET ${environment.apiUrl}/user/session`,
      `GET ${environment.apiUrl}/permission-digest`,
      `GET ${environment.apiUrl}/user-domains`,
    ]);
  });

  it('says how to add the first resource while the digest is empty', async () => {
    const fixture = await render({});
    const element: HTMLElement = fixture.nativeElement;

    expect(element.querySelectorAll('tbody tr')).toHaveLength(1);
    expect(element.querySelector('td.empty')?.textContent).toContain('No resources yet');
  });
});
