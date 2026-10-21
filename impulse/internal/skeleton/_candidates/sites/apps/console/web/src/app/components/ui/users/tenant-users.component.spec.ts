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
import { TenantUsersComponent } from './tenant-users.component';

// The tenant's Users page composes the library's user-management component over the staff
// auth's partition in the tenant the route names, drawn where that tenant's digest grants
// List on its users resource: the tenant's administrator sees the partition, read from the
// tenant's own route; a session without the grant there sees no partition, and nothing is
// requested. The route's tenant is the component's domain; nothing reads the picker.

const staff = [
  {
    id: 'u-2',
    username: 'tenant-administrator',
    disabled: false,
    hasPassword: true,
    passwordChangeRequired: false,
    createdAt: '2026-10-01T09:00:00Z',
    lastSignInAt: '2026-10-19T07:30:00Z',
    organization: null,
    identities: [],
    roles: ['Administrator'],
    everywhereRoles: [],
    rolesSynced: false,
  },
];

const tenantAdministrator: PermissionDigest = {
  StaffTenantUsers: { List: 'granted' },
  StaffTenantRoles: { List: 'granted' },
};
const nobody: PermissionDigest = {};

function serverFor(digest: PermissionDigest): (request: TransportRequest) => TransportResponse {
  return (request) => {
    const url = new URL(request.url, 'http://skeleton.test');
    switch (url.pathname) {
      case `${environment.apiUrl}/user/session`:
        return { status: 200, body: { authenticated: true, username: 'tenant-administrator' } };
      case `${environment.apiUrl}/permission-digest`:
        return { status: 200, body: url.searchParams.get('domain') === 'acme' ? digest : {} };
      case `${environment.apiUrl}/user-domains`:
        return { status: 200, body: ['acme'] };
      case `${environment.apiUrl}/features`:
        return { status: 200, body: { enabled: [] } };
      case `${environment.apiUrl}/tenants/acme/staff-tenant-users`:
        return { status: 200, body: staff };
      case `${environment.apiUrl}/tenants/acme/staff-tenant-roles`:
        return { status: 200, body: [] };
      default:
        return { status: 404, body: { message: `unscripted ${request.method} ${request.url}` } };
    }
  };
}

describe('TenantUsersComponent', () => {
  let fixture: ComponentFixture<TenantUsersComponent>;
  let transport: ScriptedTransport;

  /** Signs in, loads acme's digest as entering the tenant does, and renders the page for acme. */
  const pageFor = async (digest: PermissionDigest): Promise<void> => {
    transport = scriptedTransport(serverFor(digest));
    await TestBed.configureTestingModule({
      imports: [TenantUsersComponent],
      providers: [
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
      ],
    }).compileComponents();
    await firstValueFrom(TestBed.inject(AuthService).checkUserSession());
    await TestBed.inject<ClientBase>(RESOURCE_CLIENT).permissions.loadDigest('acme' as Domain);
    await TestBed.inject(ApplicationRef).whenStable();
    fixture = TestBed.createComponent(TenantUsersComponent);
    fixture.componentRef.setInput('tenantID', 'acme');
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

  it("the tenant's administrator sees the tenant's partition, read from the tenant's own route", async () => {
    await pageFor(tenantAdministrator);
    expect(headings()).toEqual(['Staff users of acme']);
    expect(asked()).toEqual([`${environment.apiUrl}/tenants/acme/staff-tenant-users`]);
  });

  it('a session without the List grant there sees no partition, and nothing is requested', async () => {
    await pageFor(nobody);
    expect(headings()).toEqual([]);
    expect(asked()).toEqual([]);
  });
});
