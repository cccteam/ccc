import { ApplicationRef } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { createApi } from '@app/service/zz_gen_api';
import { ClientBase, Domain, PermissionDigest, TransportRequest, TransportResponse } from '@cccteam/resource';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { RESOURCE_CLIENT } from '@cccteam/resource-angular/resource-client';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { SectorService } from '@components/sector/sector.service';
import { environment } from '@env';
import { firstValueFrom } from 'rxjs';
import { UsersComponent } from './users.component';

// The Users page composes the library's user-management component over the three
// partitions the console serves, each drawn where the digest grants List on its users
// resource: headquarters sees the crew, the crew of the selected sector and the members;
// a sector marshal sees the sector's partition alone.
//
// Demonstrates: auth.user-management.

const crew = [
  {
    id: 'u-1',
    username: 'governor',
    disabled: false,
    hasPassword: true,
    passwordChangeRequired: false,
    createdAt: '2026-10-01T09:00:00Z',
    lastSignInAt: '2026-10-19T07:30:00Z',
    organization: null,
    identities: [],
    roles: ['Headquarters'],
    everywhereRoles: ['SectorMarshal'],
    rolesSynced: false,
  },
];

/** What each persona holds: headquarters every partition, a marshal the sector's alone. */
interface Grants {
  global: PermissionDigest;
  anvil: PermissionDigest;
}

const headquarters: Grants = {
  global: { CrewUsers: { List: 'granted' }, CrewRoles: { List: 'granted' }, MembersUsers: { List: 'granted' }, MembersRoles: { List: 'granted' } },
  anvil: { CrewSectorUsers: { List: 'granted' }, CrewSectorRoles: { List: 'granted' } },
};
const marshal: Grants = {
  global: {},
  anvil: { CrewSectorUsers: { List: 'granted' }, CrewSectorRoles: { List: 'granted' } },
};

function serverFor(grants: Grants, domains: string[]): (request: TransportRequest) => TransportResponse {
  return (request) => {
    const url = new URL(request.url, 'http://lodestar.test');
    switch (url.pathname) {
      case `${environment.apiUrl}/user/session`:
        return { status: 200, body: { authenticated: true, username: 'governor' } };
      case `${environment.apiUrl}/permission-digest`:
        return { status: 200, body: url.searchParams.get('domain') === 'anvil' ? grants.anvil : grants.global };
      case `${environment.apiUrl}/user-domains`:
        return { status: 200, body: domains };
      case `${environment.apiUrl}/features`:
        return { status: 200, body: { enabled: [] } };
      case `${environment.apiUrl}/crew-users`:
      case `${environment.apiUrl}/sectors/anvil/crew-sector-users`:
        return { status: 200, body: crew };
      case `${environment.apiUrl}/members-users`:
        return { status: 200, body: [] };
      case `${environment.apiUrl}/crew-roles`:
      case `${environment.apiUrl}/sectors/anvil/crew-sector-roles`:
      case `${environment.apiUrl}/members-roles`:
        return { status: 200, body: [] };
      default:
        return { status: 404, body: { message: `unscripted ${request.method} ${request.url}` } };
    }
  };
}

describe('UsersComponent', () => {
  let fixture: ComponentFixture<UsersComponent>;
  let transport: ScriptedTransport;

  // The console selects the first sector the session holds and loads its digest by itself
  // (SectorService), so "no sector" is a session that holds none.
  const pageFor = async (grants: Grants, sector: string | undefined): Promise<void> => {
    transport = scriptedTransport(serverFor(grants, sector ? [sector] : []));
    await TestBed.configureTestingModule({
      imports: [UsersComponent],
      providers: [provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) })],
    }).compileComponents();
    await firstValueFrom(TestBed.inject(AuthService).checkUserSession());
    if (sector) {
      TestBed.inject(SectorService).select(sector);
      await TestBed.inject<ClientBase>(RESOURCE_CLIENT).permissions.loadDigest(sector as Domain);
    }
    await TestBed.inject(ApplicationRef).whenStable();
    fixture = TestBed.createComponent(UsersComponent);
    fixture.detectChanges();
    await TestBed.inject(ApplicationRef).whenStable();
    fixture.detectChanges();
  };

  const headings = (): string[] =>
    Array.from((fixture.nativeElement as HTMLElement).querySelectorAll('.ccc-users-heading')).map((h) => h.textContent?.trim() ?? '');
  const asked = (): string[] =>
    transport.requests.map((r) => r.url.replace(/\?.*$/, '')).filter((url) => /-users$/.test(url));

  it('headquarters in Anvil sees the crew, the sector\'s crew and the members, each read from its own partition', async () => {
    await pageFor(headquarters, 'anvil');
    expect(headings()).toEqual(['Crew users', 'Crew users of anvil', 'Members users']);
    expect(asked().sort()).toEqual([
      `${environment.apiUrl}/crew-users`,
      `${environment.apiUrl}/members-users`,
      `${environment.apiUrl}/sectors/anvil/crew-sector-users`,
    ]);
    expect((fixture.nativeElement as HTMLElement).querySelector('.users-note')).toBeNull();
  });

  it('with no sector selected the sector partition gives way to the note', async () => {
    await pageFor(headquarters, undefined);
    expect(headings()).toEqual(['Crew users', 'Members users']);
    expect((fixture.nativeElement as HTMLElement).querySelector('.users-note')?.textContent).toContain('Select a sector');
  });

  it('a sector marshal sees the sector\'s partition alone, and nothing else is requested', async () => {
    await pageFor(marshal, 'anvil');
    expect(headings()).toEqual(['Crew users of anvil']);
    expect(asked()).toEqual([`${environment.apiUrl}/sectors/anvil/crew-sector-users`]);
  });
});
