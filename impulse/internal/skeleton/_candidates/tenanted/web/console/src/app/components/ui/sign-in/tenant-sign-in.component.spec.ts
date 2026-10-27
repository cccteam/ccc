import { ApplicationRef } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { MatSelect } from '@angular/material/select';
import { By } from '@angular/platform-browser';
import { createApi } from '@app/service/zz_gen_api';
import { TenantDirectories } from '@app/service/zz_gen_resources';
import { ClientBase, Domain, PermissionDigest, TransportRequest, TransportResponse } from '@cccteam/resource';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { RESOURCE_CLIENT } from '@cccteam/resource-angular/resource-client';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { environment } from '@env';
import { firstValueFrom } from 'rxjs';
import { TenantSignInComponent } from './tenant-sign-in.component';

// The tenant's sign-in page shows who may enter the tenant as its declaration says, and
// lets the holder of each grant change it: the tenant's administrator (List, Create,
// Update and Delete on TenantDirectories in the tenant) lists, declares and withdraws
// directories, picks each one's default seat from the tenant's roles or clears it, and
// is told the application's administrator sets directory only; the application's
// administrator (Update on the global Tenants record too) flips the flag; a session
// without the grants sees the sentences and nothing is requested.

const workspace: TenantDirectories = { id: 'd-1', tenantId: 'acme', method: 'google', connection: 'acme.example' };
const entra: TenantDirectories = {
  id: 'd-2',
  tenantId: 'acme',
  method: 'azure',
  connection: '6f2c1b7e-3d4a-4c1e-9b2a-1f0e8d7c6b5a',
  defaultSeat: 'Administrator_Domain',
};
const directories = [workspace, entra];
const tenantRoles = [{ name: 'Administrator_Domain' }];

const administratorGrants: PermissionDigest = {
  TenantDirectories: { List: 'granted', Create: 'granted', Update: 'granted', Delete: 'granted' },
  StaffTenantRoles: { List: 'granted' },
};
const tenantAdministrator: { global: PermissionDigest; acme: PermissionDigest } = {
  global: {},
  acme: administratorGrants,
};
const applicationAdministrator: { global: PermissionDigest; acme: PermissionDigest } = {
  global: { Tenants: { Read: 'granted', Update: 'granted' } },
  acme: administratorGrants,
};
const nobody: { global: PermissionDigest; acme: PermissionDigest } = { global: {}, acme: {} };

function serverFor(digests: { global: PermissionDigest; acme: PermissionDigest }): (request: TransportRequest) => TransportResponse {
  return (request) => {
    const url = new URL(request.url, 'http://skeleton.test');
    switch (url.pathname) {
      case `${environment.apiUrl}/user/session`:
        return { status: 200, body: { authenticated: true, username: 'someone' } };
      case `${environment.apiUrl}/permission-digest`:
        return { status: 200, body: url.searchParams.get('domain') === 'acme' ? digests.acme : digests.global };
      case `${environment.apiUrl}/user-domains`:
        return { status: 200, body: ['acme'] };
      case `${environment.apiUrl}/features`:
        return { status: 200, body: { enabled: [] } };
      case `${environment.apiUrl}/tenants/acme`:
        return { status: 200, body: { id: 'acme', name: 'Acme', directoryOnly: false } };
      case `${environment.apiUrl}/tenants/acme/tenant-directories`:
        return { status: 200, body: directories };
      case `${environment.apiUrl}/tenants/acme/staff-tenant-roles`:
        return { status: 200, body: tenantRoles };
      // Every write rides the consolidated route as one operation.
      case `${environment.apiUrl}/resources`:
        return { status: 200, body: {} };
      default:
        return { status: 404, body: { message: `unscripted ${request.method} ${request.url}` } };
    }
  };
}

describe('TenantSignInComponent', () => {
  let fixture: ComponentFixture<TenantSignInComponent>;
  let transport: ScriptedTransport;

  /** Signs in, loads the global and acme digests as entering the tenant does, and renders the page for acme. */
  const pageFor = async (digests: { global: PermissionDigest; acme: PermissionDigest }): Promise<void> => {
    transport = scriptedTransport(serverFor(digests));
    await TestBed.configureTestingModule({
      imports: [TenantSignInComponent],
      providers: [
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
      ],
    }).compileComponents();
    await firstValueFrom(TestBed.inject(AuthService).checkUserSession());
    const client = TestBed.inject<ClientBase>(RESOURCE_CLIENT);
    await client.permissions.loadDigest();
    await client.permissions.loadDigest('acme' as Domain);
    await TestBed.inject(ApplicationRef).whenStable();
    fixture = TestBed.createComponent(TenantSignInComponent);
    fixture.componentRef.setInput('tenantID', 'acme');
    fixture.detectChanges();
    await TestBed.inject(ApplicationRef).whenStable();
    fixture.detectChanges();
  };

  const settle = async (): Promise<void> => {
    await TestBed.inject(ApplicationRef).whenStable();
    fixture.detectChanges();
  };
  const element = (): HTMLElement => fixture.nativeElement as HTMLElement;
  const texts = (selector: string): string[] =>
    Array.from(element().querySelectorAll(selector)).map((e) => e.textContent?.replace(/\s+/g, ' ').trim() ?? '');
  /** The operations the page sent, in order: every write is one operation on the consolidated route. */
  const writes = (): unknown[] =>
    transport.requests
      .filter((r) => r.method === 'PATCH' && r.url.replace(/\?.*$/, '') === `${environment.apiUrl}/resources`)
      .flatMap((r) => r.body as unknown[]);

  it("the tenant's administrator lists, declares and withdraws directories, and is told who sets directory only", async () => {
    await pageFor(tenantAdministrator);

    expect(texts('h1')).toEqual(['Who may enter acme']);
    expect(texts('.directories td:first-child')).toEqual(['Google Workspace', 'Microsoft Entra']);
    expect(texts('.directories code')).toEqual(['acme.example', '6f2c1b7e-3d4a-4c1e-9b2a-1f0e8d7c6b5a']);
    expect(element().querySelectorAll('.seat-picker').length).toBe(2);
    expect(element().querySelector('.declare-form')).not.toBeNull();
    expect(element().querySelector('.directory-only')).toBeNull();
    expect(texts('.empty')).toContain('Your role does not cover the tenant record, which carries this setting.');

    fixture.componentInstance.form.setValue({ method: 'google', connection: ' partner.example ' });
    await fixture.componentInstance.declare();
    await settle();
    expect(writes()).toEqual([
      { op: 'add', path: '/tenants/acme/tenant-directories', value: { method: 'google', connection: 'partner.example' } },
    ]);

    await fixture.componentInstance.withdraw(entra);
    await settle();
    expect(writes()).toEqual([
      { op: 'add', path: '/tenants/acme/tenant-directories', value: { method: 'google', connection: 'partner.example' } },
      { op: 'remove', path: '/tenants/acme/tenant-directories/d-2' },
    ]);
  });

  it("the tenant's administrator picks a directory's default seat from the tenant's roles, and clears it", async () => {
    await pageFor(tenantAdministrator);

    // The picker offers invitation only and every role of the tenant; a row with no seat
    // stands at invitation only (the empty choice), a row with one at its role.
    const pickers = fixture.debugElement.queryAll(By.css('.seat-picker')).map((d) => d.componentInstance as MatSelect);
    expect(pickers.map((p) => p.value)).toEqual(['', 'Administrator_Domain']);
    expect(pickers[0]?.options.map((o) => o.viewValue)).toEqual(['Invitation only', 'Administrator_Domain']);

    await fixture.componentInstance.seat(workspace, 'Administrator_Domain');
    await settle();
    expect(writes()).toEqual([
      { op: 'patch', path: '/tenants/acme/tenant-directories/d-1', value: { defaultSeat: 'Administrator_Domain' } },
    ]);

    // Clearing the seat sends an explicit null: the column is cleared, not left alone.
    await fixture.componentInstance.seat(entra, '');
    await settle();
    expect(writes()).toEqual([
      { op: 'patch', path: '/tenants/acme/tenant-directories/d-1', value: { defaultSeat: 'Administrator_Domain' } },
      { op: 'patch', path: '/tenants/acme/tenant-directories/d-2', value: { defaultSeat: null } },
    ]);
  });

  it('a reader without Update on the directories sees each default seat as a sentence', async () => {
    await pageFor({ global: {}, acme: { TenantDirectories: { List: 'granted' } } });

    expect(element().querySelector('.seat-picker')).toBeNull();
    expect(texts('.directories td:nth-child(3)')).toEqual(['Invitation only', 'Administrator_Domain']);
    expect(transport.requests.map((r) => r.url.replace(/\?.*$/, '')).filter((u) => u.includes('staff-tenant-roles'))).toEqual([]);
  });

  it("the application's administrator flips directory only on the tenant record", async () => {
    await pageFor(applicationAdministrator);

    const toggle = element().querySelector<HTMLElement>('.directory-only');
    expect(toggle).not.toBeNull();
    expect(toggle?.textContent?.replace(/\s+/g, ' ').trim()).toContain('Passwords admitted');
    expect(texts('.empty')).toEqual([]);

    await fixture.componentInstance.close(true);
    await settle();
    expect(writes()).toEqual([{ op: 'patch', path: '/tenants/acme', value: { directoryOnly: true } }]);
  });

  it('a session without the grants sees the sentences, and nothing is requested under the tenant', async () => {
    await pageFor(nobody);

    expect(texts('.empty')).toEqual([
      'Your role in this tenant does not cover its directories.',
      'Your role does not cover the tenant record, which carries this setting.',
    ]);
    expect(element().querySelector('.declare-form')).toBeNull();
    expect(transport.requests.map((r) => r.url.replace(/\?.*$/, '')).filter((u) => u.includes('/tenants/acme'))).toEqual([]);
  });
});
