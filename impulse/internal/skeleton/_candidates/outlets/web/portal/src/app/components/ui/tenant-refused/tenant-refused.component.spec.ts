import { ApplicationRef } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { createApi } from '@app/service/zz_gen_api';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { TransportRequest, TransportResponse } from '@cccteam/resource';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { environment } from '@env';
import { TenantRefusedComponent } from './tenant-refused.component';

// The refusal page asks the server what a page under the tenant would have asked (one row
// of the first tenant-scoped resource the API lists) and shows the answer in the server's
// words: the concealing 404 for a tenant the session cannot reach, and the way in when the
// server serves the tenant after all.

/** The server as the spec plays it: everything under tenant zzz answers the domain guard's 404. */
function server(request: TransportRequest): TransportResponse {
  const url = new URL(request.url, 'http://skeleton.test');
  if (url.pathname.startsWith(`${environment.apiUrl}/tenants/zzz/`)) {
    return { status: 404, body: { message: 'unknown domain "zzz"' } };
  }
  if (url.pathname.startsWith(`${environment.apiUrl}/tenants/acme/`)) {
    return { status: 200, body: [] };
  }
  return { status: 404, body: { message: `unscripted ${request.method} ${request.url}` } };
}

describe('TenantRefusedComponent', () => {
  let fixture: ComponentFixture<TenantRefusedComponent>;
  let transport: ScriptedTransport;

  const pageFor = async (tenant: string): Promise<void> => {
    transport = scriptedTransport(server);
    await TestBed.configureTestingModule({
      imports: [TenantRefusedComponent],
      providers: [
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
      ],
    }).compileComponents();
    fixture = TestBed.createComponent(TenantRefusedComponent);
    fixture.componentRef.setInput('tenantID', tenant);
    fixture.detectChanges();
    await TestBed.inject(ApplicationRef).whenStable();
    fixture.detectChanges();
  };

  const text = (selector: string): string | undefined =>
    (fixture.nativeElement as HTMLElement).querySelector(selector)?.textContent?.replace(/\s+/g, ' ').trim();

  const cases: { name: string; tenant: string; wantAsked: string; wantRefusal?: string; wantServed?: string }[] = [
    {
      name: "a tenant the session cannot reach shows the server's answer, in its words",
      tenant: 'zzz',
      wantAsked: `${environment.apiUrl}/tenants/zzz/announcements?limit=1`,
      wantRefusal: 'The server answered: 404: unknown domain "zzz"',
    },
    {
      name: 'a tenant the server serves after all offers the way in',
      tenant: 'acme',
      wantAsked: `${environment.apiUrl}/tenants/acme/announcements?limit=1`,
      wantServed: 'The server serves this tenant to you now. Open it.',
    },
  ];

  for (const tt of cases) {
    it(tt.name, async () => {
      await pageFor(tt.tenant);

      expect(text('h1')).toBe(`Tenant ${tt.tenant}`);
      expect(transport.requests.map((r) => r.url)).toEqual([tt.wantAsked]);
      expect(text('.refusal')).toBe(tt.wantRefusal);
      expect(text('.served')).toBe(tt.wantServed);
    });
  }
});
