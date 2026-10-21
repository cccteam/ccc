import { ApplicationRef } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { createApi } from '@app/service/zz_gen_api';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { TransportRequest, TransportResponse } from '@cccteam/resource';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { environment } from '@env';
import { SectorRefusedComponent } from './sector-refused.component';

// Demonstrates: tenancy.concealed, star-chart.
//
// The refusal page asks the server what a page under the sector would have asked (one row
// of the first sector-scoped resource the API lists) and shows the answer in the server's
// words: the concealing 404 for a sector the session cannot reach, and the way in when the
// server serves the sector after all.

/** The server as the spec plays it: everything under sector cinder answers the domain guard's 404. */
function server(request: TransportRequest): TransportResponse {
  const url = new URL(request.url, 'http://lodestar.test');
  if (url.pathname.startsWith(`${environment.apiUrl}/sectors/cinder/`)) {
    return { status: 404, body: { message: 'unknown domain "cinder"' } };
  }
  if (url.pathname.startsWith(`${environment.apiUrl}/sectors/anvil/`)) {
    return { status: 200, body: [] };
  }
  return { status: 404, body: { message: `unscripted ${request.method} ${request.url}` } };
}

describe('SectorRefusedComponent', () => {
  let fixture: ComponentFixture<SectorRefusedComponent>;
  let transport: ScriptedTransport;

  const pageFor = async (sector: string): Promise<void> => {
    transport = scriptedTransport(server);
    await TestBed.configureTestingModule({
      imports: [SectorRefusedComponent],
      providers: [
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
      ],
    }).compileComponents();
    fixture = TestBed.createComponent(SectorRefusedComponent);
    fixture.componentRef.setInput('sectorID', sector);
    fixture.detectChanges();
    await TestBed.inject(ApplicationRef).whenStable();
    fixture.detectChanges();
  };

  const text = (selector: string): string | undefined =>
    (fixture.nativeElement as HTMLElement).querySelector(selector)?.textContent?.replace(/\s+/g, ' ').trim();

  const cases: { name: string; sector: string; wantAsked: string; wantRefusal?: string; wantServed?: string }[] = [
    {
      name: "a sector the session cannot reach shows the server's answer, in its words",
      sector: 'cinder',
      wantAsked: `${environment.apiUrl}/sectors/cinder/client-rosters?limit=1`,
      wantRefusal: 'The server answered: 404: unknown domain "cinder"',
    },
    {
      name: 'a sector the server serves after all offers the way in',
      sector: 'anvil',
      wantAsked: `${environment.apiUrl}/sectors/anvil/client-rosters?limit=1`,
      wantServed: 'The server serves this sector to you now. Open it.',
    },
  ];

  for (const tt of cases) {
    it(tt.name, async () => {
      await pageFor(tt.sector);

      expect(text('h1')).toBe(`Sector ${tt.sector}`);
      expect(transport.requests.map((r) => r.url)).toEqual([tt.wantAsked]);
      expect(text('.refusal')).toBe(tt.wantRefusal);
      expect(text('.served')).toBe(tt.wantServed);
    });
  }
});
