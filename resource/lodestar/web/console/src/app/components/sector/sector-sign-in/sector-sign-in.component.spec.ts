import { ApplicationRef } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { createApi } from '@app/service/zz_gen_api';
import { SectorDirectories } from '@app/service/zz_gen_resources';
import { ClientBase, Domain, PermissionDigest, TransportRequest, TransportResponse } from '@cccteam/resource';
import { AuthService } from '@cccteam/resource-angular/auth-service';
import { RESOURCE_CLIENT } from '@cccteam/resource-angular/resource-client';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { environment } from '@env';
import { firstValueFrom } from 'rxjs';
import { SectorSignInComponent } from './sector-sign-in.component';

// Demonstrates: tenancy.declaration, tenancy.directory-only.
//
// The sector's sign-in page shows who may enter the sector as its declaration says, and
// lets the holder of each grant change it: the sector's marshal (List, Create and
// Delete on SectorDirectories in the sector) lists, declares and withdraws directories and
// is told headquarters sets directory only; headquarters (Update on the global Sectors
// record too) flips the flag; a session without the grants sees the sentences and
// nothing is requested.

const workspace: SectorDirectories = { id: 'd-1', sectorId: 'anvil', method: 'google', connection: 'example' };
const entra: SectorDirectories = {
  id: 'd-2',
  sectorId: 'anvil',
  method: 'azure',
  connection: '6f2c1b7e-3d4a-4c1e-9b2a-1f0e8d7c6b5a',
};
const directories = [workspace, entra];

const marshal: { global: PermissionDigest; anvil: PermissionDigest } = {
  global: {},
  anvil: { SectorDirectories: { List: 'granted', Create: 'granted', Delete: 'granted' } },
};
const headquarters: { global: PermissionDigest; anvil: PermissionDigest } = {
  global: { Sectors: { Read: 'granted', Update: 'granted' } },
  anvil: { SectorDirectories: { List: 'granted', Create: 'granted', Delete: 'granted' } },
};
const nobody: { global: PermissionDigest; anvil: PermissionDigest } = { global: {}, anvil: {} };

function serverFor(digests: { global: PermissionDigest; anvil: PermissionDigest }): (request: TransportRequest) => TransportResponse {
  return (request) => {
    const url = new URL(request.url, 'http://lodestar.test');
    switch (url.pathname) {
      case `${environment.apiUrl}/user/session`:
        return { status: 200, body: { authenticated: true, username: 'someone' } };
      case `${environment.apiUrl}/permission-digest`:
        return { status: 200, body: url.searchParams.get('domain') === 'anvil' ? digests.anvil : digests.global };
      case `${environment.apiUrl}/user-domains`:
        return { status: 200, body: ['anvil'] };
      case `${environment.apiUrl}/features`:
        return { status: 200, body: { enabled: [] } };
      case `${environment.apiUrl}/sectors/anvil`:
        return { status: 200, body: { id: 'anvil', name: 'Anvil', directoryOnly: false } };
      case `${environment.apiUrl}/sectors/anvil/sector-directories`:
        return { status: 200, body: directories };
      // Every write rides the consolidated route as one operation.
      case `${environment.apiUrl}/resources`:
        return { status: 200, body: {} };
      default:
        return { status: 404, body: { message: `unscripted ${request.method} ${request.url}` } };
    }
  };
}

describe('SectorSignInComponent', () => {
  let fixture: ComponentFixture<SectorSignInComponent>;
  let transport: ScriptedTransport;

  /** Signs in, loads the global and anvil digests as entering the sector does, and renders the page for anvil. */
  const pageFor = async (digests: { global: PermissionDigest; anvil: PermissionDigest }): Promise<void> => {
    transport = scriptedTransport(serverFor(digests));
    await TestBed.configureTestingModule({
      imports: [SectorSignInComponent],
      providers: [
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
      ],
    }).compileComponents();
    await firstValueFrom(TestBed.inject(AuthService).checkUserSession());
    const client = TestBed.inject<ClientBase>(RESOURCE_CLIENT);
    await client.permissions.loadDigest();
    await client.permissions.loadDigest('anvil' as Domain);
    await TestBed.inject(ApplicationRef).whenStable();
    fixture = TestBed.createComponent(SectorSignInComponent);
    fixture.componentRef.setInput('sectorID', 'anvil');
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

  it("the sector's marshal lists, declares and withdraws directories, and is told who sets directory only", async () => {
    await pageFor(marshal);

    expect(texts('h1')).toEqual(['Who may enter anvil']);
    expect(texts('.directories td:first-child')).toEqual(['Google Workspace', 'Microsoft Entra']);
    expect(texts('.directories code')).toEqual(['example', '6f2c1b7e-3d4a-4c1e-9b2a-1f0e8d7c6b5a']);
    expect(element().querySelector('.declare-form')).not.toBeNull();
    expect(element().querySelector('.directory-only')).toBeNull();
    expect(texts('.empty')).toContain('Your role does not cover the sector record, which carries this setting.');

    fixture.componentInstance.form.setValue({ method: 'google', connection: ' partner.example ' });
    await fixture.componentInstance.declare();
    await settle();
    expect(writes()).toEqual([
      { op: 'add', path: '/sectors/anvil/sector-directories', value: { method: 'google', connection: 'partner.example' } },
    ]);

    await fixture.componentInstance.withdraw(entra);
    await settle();
    expect(writes()).toEqual([
      { op: 'add', path: '/sectors/anvil/sector-directories', value: { method: 'google', connection: 'partner.example' } },
      { op: 'remove', path: '/sectors/anvil/sector-directories/d-2' },
    ]);
  });

  it("headquarters flips directory only on the sector record", async () => {
    await pageFor(headquarters);

    const toggle = element().querySelector<HTMLElement>('.directory-only');
    expect(toggle).not.toBeNull();
    expect(toggle?.textContent?.replace(/\s+/g, ' ').trim()).toContain('Passwords admitted');
    expect(texts('.empty')).toEqual([]);

    await fixture.componentInstance.close(true);
    await settle();
    expect(writes()).toEqual([{ op: 'patch', path: '/sectors/anvil', value: { directoryOnly: true } }]);
  });

  it('a session without the grants sees the sentences, and nothing is requested under the sector', async () => {
    await pageFor(nobody);

    expect(texts('.empty')).toEqual([
      'Your role in this sector does not cover its directories.',
      'Your role does not cover the sector record, which carries this setting.',
    ]);
    expect(element().querySelector('.declare-form')).toBeNull();
    expect(transport.requests.map((r) => r.url.replace(/\?.*$/, '')).filter((u) => u.includes('/sectors/anvil'))).toEqual([]);
  });
});
