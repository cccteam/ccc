import { ComponentFixture, TestBed } from '@angular/core/testing';
import { createApi } from '@app/service/zz_gen_api';
import { DomainRoutePath } from '@app/service/zz_gen_resources';
import { MenuItem } from '@cccteam/resource-angular/types';
import { SectorService } from '@components/sector/sector.service';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { environment } from '@env';
import { TopbarComponent } from './topbar.component';

describe('TopbarComponent', () => {
  let fixture: ComponentFixture<TopbarComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [TopbarComponent],
      // The sector service scopes each item's permission question through the generated client.
      providers: [provideResourceTesting({ client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) })],
    }).compileComponents();

    fixture = TestBed.createComponent(TopbarComponent);
    fixture.detectChanges();
  });

  it('creates the menu over the generated navigation items', () => {
    expect(fixture.componentInstance).toBeTruthy();
    expect(fixture.nativeElement.querySelector('nav.menu')).not.toBeNull();
  });

  // An item under the sector segment names the segment's route parameter: the open sector
  // fills it, and outside a sector the item has no link and is not drawn; a global item's
  // route is its own.
  const cases: { name: string; route: string[]; sector: string; want: string[] | undefined }[] = [
    { name: 'a global item keeps its route', route: ['users'], sector: 'anvil', want: ['users'] },
    { name: 'a global item keeps its route outside a sector', route: ['users'], sector: '', want: ['users'] },
    {
      name: 'a deck lands under the open sector',
      route: [DomainRoutePath, 'flight-deck'],
      sector: 'anvil',
      want: ['sectors/anvil', 'flight-deck'],
    },
    {
      name: 'a config-driven page registered at its route lands under the open sector',
      route: [`${DomainRoutePath}/missions`],
      sector: 'anvil',
      want: ['sectors/anvil/missions'],
    },
    {
      name: 'a sector item has no link outside a sector',
      route: [DomainRoutePath, 'flight-deck'],
      sector: '',
      want: undefined,
    },
  ];

  for (const tt of cases) {
    it(tt.name, () => {
      // Entered as the sector guard does; the top bar itself writes nothing.
      TestBed.inject(SectorService).enter(tt.sector);
      const item: MenuItem = { label: 'Deck', route: tt.route };

      expect(fixture.componentInstance.linkFor(item)).toEqual(tt.want);
    });
  }
});
