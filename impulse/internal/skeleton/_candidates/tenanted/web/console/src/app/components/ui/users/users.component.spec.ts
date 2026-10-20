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
import { UsersComponent } from './users.component';

// The Users page composes the library's user-management component over the two partitions
// the console serves, each drawn where the digest grants List on its users resource: a
// global administrator sees the staff and the staff of the selected tenant; a tenant's
// administrator sees the tenant's partition alone.

const staff = [
  {
    id: 'u-1',
    username: 'administrator',
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

/** What each persona holds: the global administrator both partitions, the tenant's its own. */
interface Grants {
  global: PermissionDigest;
  acme: PermissionDigest;
}

const administrator: Grants = {
  global: { StaffUsers: { List: 'granted' }, StaffRoles: { List: 'granted' } },
  acme: { StaffTenantUsers: { List: 'granted' }, StaffTenantRoles: { List: 'granted' } },
};
const tenantAdministrator: Grants = {
  global: {},
  acme: { StaffTenantUsers: { List: 'granted' }, StaffTenantRoles: { List: 'granted' } },
};

function serverFor(grants: Grants, tenants: string[]): (request: TransportRequest) => TransportResponse {
  return (request) => {
    const url = new URL(request.url, 'http://skeleton.test');
    switch (url.pathname) {
      case `${environment.apiUrl}/user/session`:
        return { status: 200, body: { authenticated: true, username: 'administrator' } };
      case `${environment.apiUrl}/permission-digest`:
        return { status: 200, body: url.searchParams.get('domain') === 'acme' ? grants.acme : grants.global };
      case `${environment.apiUrl}/user-domains`:
        return { status: 200, body: tenants };
      case `${environment.apiUrl}/features`:
        return { status: 200, body: { enabled: [] } };
      case `${environment.apiUrl}/staff-users`:
      case `${environment.apiUrl}/tenants/acme/staff-tenant-users`:
        return { status: 200, body: staff };
      case `${environment.apiUrl}/staff-roles`:
      case `${environment.apiUrl}/tenants/acme/staff-tenant-roles`:
        return { status: 200, body: [] };
      default:
        return { status: 404, body: { message: `unscripted ${request.method} ${request.url}` } };
    }
  };
}

describe('UsersComponent', () => {
  let fixture: ComponentFixture<UsersComponent>;
  let transport: ScriptedTransport;

  // The console selects the first tenant the session holds and loads its digest by itself
  // (TenantService), so "no tenant" is a session that holds none.
  const pageFor = async (grants: Grants, tenant: string | undefined): Promise<void> => {
    transport = scriptedTransport(serverFor(grants, tenant ? [tenant] : []));
    await TestBed.configureTestingModule({
      imports: [UsersComponent],
      providers: [provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) })],
    }).compileComponents();
    await firstValueFrom(TestBed.inject(AuthService).checkUserSession());
    if (tenant) {
      await TestBed.inject<ClientBase>(RESOURCE_CLIENT).permissions.loadDigest(tenant as Domain);
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

  it('a global administrator in acme sees the staff and the tenant\'s staff, each read from its own partition', async () => {
    await pageFor(administrator, 'acme');
    expect(headings()).toEqual(['Staff users', 'Staff users of acme']);
    expect(asked().sort()).toEqual([`${environment.apiUrl}/staff-users`, `${environment.apiUrl}/tenants/acme/staff-tenant-users`]);
    expect((fixture.nativeElement as HTMLElement).querySelector('.users-note')).toBeNull();
  });

  it('with no tenant selected the tenant partition gives way to the note', async () => {
    await pageFor(administrator, undefined);
    expect(headings()).toEqual(['Staff users']);
    expect((fixture.nativeElement as HTMLElement).querySelector('.users-note')?.textContent).toContain('Select a tenant');
  });

  it('a tenant\'s administrator sees the tenant\'s partition alone, and nothing else is requested', async () => {
    await pageFor(tenantAdministrator, 'acme');
    expect(headings()).toEqual(['Staff users of acme']);
    expect(asked()).toEqual([`${environment.apiUrl}/tenants/acme/staff-tenant-users`]);
  });
});
