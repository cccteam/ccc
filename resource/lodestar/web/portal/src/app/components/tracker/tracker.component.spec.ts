import { ComponentFixture, TestBed } from '@angular/core/testing';
import { createApi } from '@app/service/zz_gen_api';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { ScriptedTransport, scriptedTransport } from '@cccteam/resource/testing';
import { environment } from '@env';
import { TrackerComponent } from './tracker.component';

describe('TrackerComponent', () => {
  let fixture: ComponentFixture<TrackerComponent>;
  let transport: ScriptedTransport;

  beforeEach(async () => {
    transport = scriptedTransport();
    await TestBed.configureTestingModule({
      imports: [TrackerComponent],
      // The tracker reads its handles and the page size off the portal's generated client.
      providers: [
        provideResourceTesting({ transport, client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(TrackerComponent);
    fixture.detectChanges();
    // The resources' loaders run once the microtasks drain; whenStable would wait on the idle
    // service's keepalive interval instead, which never settles.
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
  });

  it('creates the tracker with the page size the descriptor declares for missions', () => {
    const element: HTMLElement = fixture.nativeElement;
    expect(element.querySelector('.brand')?.textContent).toContain('Lodestar Client Portal');
    expect(element.querySelector('h1')?.textContent).toContain('Your missions in');
    expect(fixture.componentInstance.pageSize).toBeGreaterThan(0);
  });

  it('asks the server for nothing before a sector is known and a grant is held', () => {
    expect(fixture.componentInstance.sectors()).toEqual([]);
    expect(fixture.componentInstance.canFile()).toBe(false);
    expect(transport.requests).toEqual([]);
  });
});
