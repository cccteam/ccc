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

// The Users page composes the library's user-management component over the members auth's
// surface, drawn where the digest grants List on its users resource: an administrator sees
// the members partition, read from its own route; a session without the grant sees no
// partition, and nothing is requested.

const members = [
  {
    id: 'u-1',
    username: 'member',
    disabled: false,
    hasPassword: false,
    passwordChangeRequired: false,
    createdAt: '2026-10-01T09:00:00Z',
    lastSignInAt: '2026-10-19T07:30:00Z',
    organization: null,
    identities: [],
    roles: ['Administrator'],
    everywhereRoles: [],
    rolesSynced: true,
  },
];

const administrator: PermissionDigest = { MembersUsers: { List: 'granted' }, MembersRoles: { List: 'granted' } };
const nobody: PermissionDigest = {};

function serverFor(digest: PermissionDigest): (request: TransportRequest) => TransportResponse {
  return (request) => {
    const url = new URL(request.url, 'http://skeleton.test');
    switch (url.pathname) {
      case `${environment.apiUrl}/user/session`:
        return { status: 200, body: { authenticated: true, username: 'member' } };
      case `${environment.apiUrl}/permission-digest`:
        return { status: 200, body: digest };
      case `${environment.apiUrl}/user-domains`:
        return { status: 200, body: [] };
      case `${environment.apiUrl}/features`:
        return { status: 200, body: { enabled: [] } };
      case `${environment.apiUrl}/members-users`:
        return { status: 200, body: members };
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
      providers: [provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) })],
    }).compileComponents();
    await firstValueFrom(TestBed.inject(AuthService).checkUserSession());
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

  it('an administrator sees the members partition, read from the members users route', async () => {
    await pageFor(administrator);
    expect(headings()).toEqual(['Members users']);
    expect(asked()).toEqual([`${environment.apiUrl}/members-users`]);
  });

  it('a session without the List grant sees no partition, and nothing is requested', async () => {
    await pageFor(nobody);
    expect(headings()).toEqual([]);
    expect(asked()).toEqual([]);
  });
});
