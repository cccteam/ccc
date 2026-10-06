import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
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

/** Anvil's digest for a marshal: every mission permission, ships read under terms. */
const anvil: PermissionDigest = {
  Missions: { List: 'granted', Read: 'granted', Create: 'granted', Update: 'granted', Delete: 'granted' },
  Ships: { List: 'conditional', Read: 'conditional' },
};

/**
 * The server as the spec plays it: the session, the global digest (empty: a marshal holds
 * nothing outside a sector), the user's domains, and the digest of the sector asked for.
 * Anything else is a 404 the request log would show.
 */
function server(request: TransportRequest): TransportResponse {
  const url = new URL(request.url, 'http://lodestar.test');
  switch (url.pathname) {
    case `${environment.apiUrl}/user/session`:
      return { status: 200, body: { authenticated: true, username: 'marshal' } };
    case `${environment.apiUrl}/permission-digest`:
      return { status: 200, body: url.searchParams.get('domain') === 'anvil' ? anvil : {} };
    case `${environment.apiUrl}/user-domains`:
      return { status: 200, body: ['anvil'] };
    default:
      return { status: 404, body: { message: `unscripted ${request.method} ${request.url}` } };
  }
}

describe('DashboardComponent', () => {
  let fixture: ComponentFixture<DashboardComponent>;
  let transport: ScriptedTransport;

  beforeEach(async () => {
    transport = scriptedTransport(server);
    await TestBed.configureTestingModule({
      imports: [DashboardComponent],
      providers: [
        // The console's generated client over the scripted transport, so the service card reads
        // the digest as it does in the browser; the impersonation service posts through HttpClient.
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
        provideHttpClient(),
        provideHttpClientTesting(),
      ],
    }).compileComponents();

    // Signing in loads the session, the global digest, and the domains: what the login page
    // runs. Selecting the one lit sector loads its digest, as SectorService does on selection.
    await firstValueFrom(TestBed.inject(AuthService).checkUserSession());
    await TestBed.inject<ClientBase>(RESOURCE_CLIENT).permissions.loadDigest('anvil' as Domain);

    fixture = TestBed.createComponent(DashboardComponent);
    fixture.detectChanges();
  });

  it('renders the service card for the lit sector from its digest', () => {
    const element: HTMLElement = fixture.nativeElement;
    expect(element.querySelector('h1')?.textContent).toContain('Welcome aboard, marshal');
    expect(element.querySelector('.card-badge mat-card-title')?.textContent).toContain('Service card — anvil');

    const rows = Array.from(element.querySelectorAll('table.badge tbody tr'));
    const missions = rows.find((row) => row.querySelector('td.label')?.textContent?.trim().startsWith('Missions'));
    const ships = rows.find((row) => row.querySelector('td.label')?.textContent?.trim().startsWith('Ships'));
    // The columns are List, Read, Create, Update, Delete.
    const pills = (row: Element | undefined): string[] =>
      Array.from(row?.querySelectorAll('td:not(.label) .pill') ?? []).map((pill) => pill.textContent?.trim() ?? '');
    expect(pills(missions)).toEqual(['granted', 'granted', 'granted', 'granted', 'granted']);
    expect(pills(ships)).toEqual(['terms apply', 'terms apply', '—', '—', '—']);
  });

  it('asked the server for the session, both digests, and the domains, and nothing it was not granted', () => {
    const asked = transport.requests.map((request) => `${request.method} ${request.url}`);
    expect(asked).toContain(`GET ${environment.apiUrl}/user/session`);
    expect(asked).toContain(`GET ${environment.apiUrl}/permission-digest`);
    expect(asked).toContain(`GET ${environment.apiUrl}/user-domains`);
    expect(asked).toContain(`GET ${environment.apiUrl}/permission-digest?domain=anvil`);
    // The service ledger is a global list the marshal holds no List grant on: never requested.
    expect(asked.filter((line) => line.includes('service-ledgers'))).toEqual([]);
  });
});
