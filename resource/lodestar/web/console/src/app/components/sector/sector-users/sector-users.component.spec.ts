import { ApplicationRef } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { createApi } from '@app/service/zz_gen_api';
import { ClientBase, Domain, PermissionDigest, TransportRequest, TransportResponse } from '@cccteam/resource';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { RESOURCE_CLIENT } from '@cccteam/resource-angular/resource-client';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { environment } from '@env';
import { firstValueFrom } from 'rxjs';
import { SectorUsersComponent } from './sector-users.component';

// Demonstrates: auth.user-management.
//
// The sector's crew page composes the library's user-management component over the crew
// auth's partition in the sector the route names, drawn where that sector's digest grants
// List on its users resource: the marshal sees the partition, read from the
// sector's own route; a session without the grant there sees no partition, and nothing is
// requested. The route's sector is the component's domain; nothing reads the picker.

const staff = [
  {
    id: 'u-2',
    username: 'marshal',
    disabled: false,
    hasPassword: true,
    passwordChangeRequired: false,
    createdAt: '2026-10-01T09:00:00Z',
    lastSignInAt: '2026-10-19T07:30:00Z',
    organization: null,
    identities: [],
    roles: ['SectorMarshal'],
    everywhereRoles: [],
    rolesSynced: false,
  },
];

const marshal: PermissionDigest = { CrewSectorUsers: { List: 'granted' }, CrewSectorRoles: { List: 'granted' } };
const nobody: PermissionDigest = {};

function serverFor(digest: PermissionDigest): (request: TransportRequest) => TransportResponse {
  return (request) => {
    const url = new URL(request.url, 'http://lodestar.test');
    switch (url.pathname) {
      case `${environment.apiUrl}/user/session`:
        return { status: 200, body: { authenticated: true, username: 'marshal' } };
      case `${environment.apiUrl}/permission-digest`:
        return { status: 200, body: url.searchParams.get('domain') === 'anvil' ? digest : {} };
      case `${environment.apiUrl}/user-domains`:
        return { status: 200, body: ['anvil'] };
      case `${environment.apiUrl}/features`:
        return { status: 200, body: { enabled: [] } };
      case `${environment.apiUrl}/sectors/anvil/crew-sector-users`:
        return { status: 200, body: staff };
      case `${environment.apiUrl}/sectors/anvil/crew-sector-roles`:
        return { status: 200, body: [] };
      default:
        return { status: 404, body: { message: `unscripted ${request.method} ${request.url}` } };
    }
  };
}

describe('SectorUsersComponent', () => {
  let fixture: ComponentFixture<SectorUsersComponent>;
  let transport: ScriptedTransport;

  /** Signs in, loads anvil's digest as entering the sector does, and renders the page for anvil. */
  const pageFor = async (digest: PermissionDigest): Promise<void> => {
    transport = scriptedTransport(serverFor(digest));
    await TestBed.configureTestingModule({
      imports: [SectorUsersComponent],
      providers: [
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
      ],
    }).compileComponents();
    await firstValueFrom(TestBed.inject(AuthService).checkUserSession());
    await TestBed.inject<ClientBase>(RESOURCE_CLIENT).permissions.loadDigest('anvil' as Domain);
    await TestBed.inject(ApplicationRef).whenStable();
    fixture = TestBed.createComponent(SectorUsersComponent);
    fixture.componentRef.setInput('sectorID', 'anvil');
    fixture.detectChanges();
    await TestBed.inject(ApplicationRef).whenStable();
    fixture.detectChanges();
  };

  const headings = (): string[] =>
    Array.from((fixture.nativeElement as HTMLElement).querySelectorAll('.ccc-users-heading')).map(
      (h) => h.textContent?.trim() ?? '',
    );
  const asked = (): string[] =>
    transport.requests.map((r) => r.url.replace(/\?.*$/, '')).filter((url) => /-users$/.test(url));

  it("the sector's administrator sees the sector's partition, read from the sector's own route", async () => {
    await pageFor(marshal);
    expect(headings()).toEqual(['Crew users of anvil']);
    expect(asked()).toEqual([`${environment.apiUrl}/sectors/anvil/crew-sector-users`]);
  });

  it('a session without the List grant there sees no partition, and nothing is requested', async () => {
    await pageFor(nobody);
    expect(headings()).toEqual([]);
    expect(asked()).toEqual([]);
  });
});
