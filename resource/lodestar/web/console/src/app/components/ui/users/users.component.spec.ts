import { ApplicationRef } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { createApi } from '@app/service/zz_gen_api';
import { PermissionDigest, TransportRequest, TransportResponse } from '@cccteam/resource';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { environment } from '@env';
import { firstValueFrom } from 'rxjs';
import { UsersComponent } from './users.component';

// The Users page composes the library's user-management component over the global
// partitions the console serves, each drawn where the digest grants List on its users
// resource: headquarters sees the crew and the members; a sector marshal, who holds
// nothing outside a sector, sees neither, and nothing is requested. The crew of a sector
// is the sector's own page (SectorUsersComponent).
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

/** What each persona holds globally: headquarters both partitions, a marshal nothing. */
const headquarters: PermissionDigest = {
  CrewUsers: { List: 'granted' },
  CrewRoles: { List: 'granted' },
  MembersUsers: { List: 'granted' },
  MembersRoles: { List: 'granted' },
};
const marshal: PermissionDigest = {};

function serverFor(digest: PermissionDigest): (request: TransportRequest) => TransportResponse {
  return (request) => {
    const url = new URL(request.url, 'http://lodestar.test');
    switch (url.pathname) {
      case `${environment.apiUrl}/user/session`:
        return { status: 200, body: { authenticated: true, username: 'governor' } };
      case `${environment.apiUrl}/permission-digest`:
        return { status: 200, body: digest };
      case `${environment.apiUrl}/user-domains`:
        return { status: 200, body: [] };
      case `${environment.apiUrl}/features`:
        return { status: 200, body: { enabled: [] } };
      case `${environment.apiUrl}/crew-users`:
        return { status: 200, body: crew };
      case `${environment.apiUrl}/members-users`:
        return { status: 200, body: [] };
      case `${environment.apiUrl}/crew-roles`:
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

  const pageFor = async (digest: PermissionDigest): Promise<void> => {
    transport = scriptedTransport(serverFor(digest));
    await TestBed.configureTestingModule({
      imports: [UsersComponent],
      providers: [
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
      ],
    }).compileComponents();
    await firstValueFrom(TestBed.inject(AuthService).checkUserSession());
    await TestBed.inject(ApplicationRef).whenStable();
    fixture = TestBed.createComponent(UsersComponent);
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

  it('headquarters sees the crew and the members, each read from its own partition', async () => {
    await pageFor(headquarters);
    expect(headings()).toEqual(['Crew users', 'Members users']);
    expect(asked().sort()).toEqual([`${environment.apiUrl}/crew-users`, `${environment.apiUrl}/members-users`]);
  });

  it('a sector marshal holds nothing outside a sector: no partition, and nothing is requested', async () => {
    await pageFor(marshal);
    expect(headings()).toEqual([]);
    expect(asked()).toEqual([]);
  });
});
