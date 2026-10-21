import { ComponentFixture, TestBed } from '@angular/core/testing';
import { createApi } from '@app/service/zz_gen_api';
import { DomainRoutePath } from '@app/service/zz_gen_resources';
import { TenantService } from '@app/tenant/tenant.service';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { MenuItem } from '@cccteam/resource-angular/types';
import { environment } from '@env';
import { TopbarComponent } from './topbar.component';

describe('TopbarComponent', () => {
  let fixture: ComponentFixture<TopbarComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [TopbarComponent],
      providers: [provideResourceTesting({ client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) })],
    }).compileComponents();

    fixture = TestBed.createComponent(TopbarComponent);
    fixture.detectChanges();
  });

  it('creates the menu over the generated navigation items', () => {
    expect(fixture.componentInstance).toBeTruthy();
    expect(fixture.nativeElement.querySelector('nav.menu')).not.toBeNull();
  });

  // An item under the tenant segment names the segment's route parameter: the open tenant
  // fills it, and outside a tenant the item has no link and is not drawn; a global item's
  // route is its own.
  const cases: { name: string; route: string[]; tenant: string; want: string[] | undefined }[] = [
    { name: 'a global item keeps its route', route: ['users'], tenant: 'acme', want: ['users'] },
    { name: 'a global item keeps its route outside a tenant', route: ['users'], tenant: '', want: ['users'] },
    {
      name: 'a tenant item lands under the open tenant',
      route: [DomainRoutePath, 'users'],
      tenant: 'acme',
      want: ['tenants/acme', 'users'],
    },
    {
      name: 'a tenant item has no link outside a tenant',
      route: [DomainRoutePath, 'users'],
      tenant: '',
      want: undefined,
    },
  ];

  for (const tt of cases) {
    it(tt.name, () => {
      // Entered as the tenant guard does; the top bar itself writes nothing.
      TestBed.inject(TenantService).enter(tt.tenant);
      const item: MenuItem = { label: 'Users', route: tt.route };

      expect(fixture.componentInstance.linkFor(item)).toEqual(tt.want);
    });
  }
});
