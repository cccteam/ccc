import { ApplicationRef } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ActivatedRoute } from '@angular/router';
import { createApi } from '@app/service/zz_gen_api';
import { resourceMeta } from '@app/service/zz_gen_resources';
import { ClientBase, TransportRequest, TransportResponse } from '@cccteam/resource';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { CompoundResourceComponent, ResourceListComponent } from '@cccteam/resource-angular/ccc-resource';
import { RESOURCE_CLIENT } from '@cccteam/resource-angular/resource-client';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { RESOURCE_META } from '@cccteam/resource-angular/types';
import { environment } from '@env';
import { clientsConfig } from './clients.config';

// The clients list's Trusted column is the library's icon column over the declared map:
// a trusted outfit draws the verified badge, one not yet trusted the caution, each named
// by the sentence with the client's name filled in, and the header still sorts on the field.
//
// Demonstrates: column.icon.

const roster = [
  { id: 'c-1', name: 'Halvard Freight', contactName: 'Ingrid Halvard', trusted: true },
  { id: 'c-2', name: 'Bastion Relay Station', contactName: 'Relay Desk', trusted: false },
];

function server(request: TransportRequest): TransportResponse {
  const url = new URL(request.url, 'http://lodestar.test');
  switch (url.pathname) {
    case `${environment.apiUrl}/permission-digest`:
      return { status: 200, body: { Clients: { List: 'granted', Read: 'granted' } } };
    case `${environment.apiUrl}/user-domains`:
      return { status: 200, body: [] };
    case `${environment.apiUrl}/clients`:
      return { status: 200, body: roster, headers: { 'total-count': '2' } };
    default:
      return { status: 404, body: { message: `unscripted ${request.method} ${request.url}` } };
  }
}

describe('the clients list', () => {
  let fixture: ComponentFixture<ResourceListComponent>;
  let transport: ScriptedTransport;

  beforeEach(async () => {
    transport = scriptedTransport(server);
    await TestBed.configureTestingModule({
      imports: [ResourceListComponent],
      providers: [
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
        { provide: RESOURCE_META, useValue: resourceMeta },
        { provide: ActivatedRoute, useValue: { snapshot: { data: { config: clientsConfig }, params: {}, queryParams: {} } } },
      ],
    }).compileComponents();
    await TestBed.inject<ClientBase>(RESOURCE_CLIENT).permissions.loadDigest();
    fixture = TestBed.createComponent(ResourceListComponent);
    fixture.componentRef.setInput('compoundResourceComponent', CompoundResourceComponent);
    fixture.componentRef.setInput('resourceConfig', clientsConfig.parentConfig);
    fixture.detectChanges();
    await TestBed.inject(ApplicationRef).whenStable();
    for (let i = 0; i < 50 && fixture.componentInstance.store.pageStatus() !== 'resolved'; i++) {
      await new Promise((resolve) => setTimeout(resolve, 0));
    }
    fixture.detectChanges();
  });

  it('draws the trusted flag as a badge named by the sentence, requesting the fields the sentence names', () => {
    const element: HTMLElement = fixture.nativeElement;
    const cells = Array.from(element.querySelectorAll<HTMLElement>('tr.ccc-row td.icon-col'));
    expect(cells.map((cell) => cell.querySelector('mat-icon')?.textContent?.trim())).toEqual(['verified', 'gpp_maybe']);
    expect(cells.map((cell) => cell.querySelector('mat-icon')?.getAttribute('aria-label'))).toEqual([
      'Halvard Freight is a trusted client',
      'Bastion Relay Station is not yet trusted',
    ]);
    const list = transport.requests.find((request) => request.url.startsWith(`${environment.apiUrl}/clients?`));
    const columns = new URL(list?.url ?? '', 'http://lodestar.test').searchParams.get('columns')?.split(',').sort();
    expect(columns).toEqual(['contactName', 'id', 'name', 'trusted']);
    expect(Array.from(element.querySelectorAll('th .col-header')).map((h) => h.textContent?.trim())).toContain('Trust');
  });
});
