import { Component } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';
import { createApi } from '@app/service/zz_gen_api';
import { provideResourceTesting } from '@cccteam/resource-angular/testing';
import { environment } from '@env';
import { TenantService } from './tenant.service';

// The picker's action: open(tenant) navigates to the page the browser is on, under the
// chosen tenant. A page under a tenant keeps its path with the tenant replaced, a page
// outside one enters the tenant on that path, the refusal page opens the tenant's landing,
// and the query travels along. The service writes no tenant itself: the guard on the
// segment does, from the new URL.

@Component({ template: 'any page' })
class AnyPageComponent {}

describe('TenantService.open', () => {
  const cases: { name: string; from: string; tenant: string; want: string }[] = [
    {
      name: 'a page under a tenant keeps its path under the new tenant',
      from: '/tenants/acme/users',
      tenant: 'beta',
      want: '/tenants/beta/users',
    },
    {
      name: 'the query travels along',
      from: '/tenants/acme/users?q=1',
      tenant: 'beta',
      want: '/tenants/beta/users?q=1',
    },
    {
      name: 'a page outside a tenant enters the tenant on that path',
      from: '/users',
      tenant: 'acme',
      want: '/tenants/acme/users',
    },
    {
      name: "the dashboard outside a tenant opens the tenant's dashboard",
      from: '/dashboard',
      tenant: 'acme',
      want: '/tenants/acme/dashboard',
    },
    {
      name: "the refusal page opens the tenant's landing",
      from: '/tenants/zzz/refused',
      tenant: 'beta',
      want: '/tenants/beta/dashboard',
    },
  ];

  for (const tt of cases) {
    it(tt.name, async () => {
      TestBed.configureTestingModule({
        providers: [
          provideResourceTesting({ client: (t) => createApi({ baseUrl: environment.apiUrl, transport: t }) }),
          provideRouter([{ path: '**', component: AnyPageComponent }]),
        ],
      });
      const harness = await RouterTestingHarness.create();
      await harness.navigateByUrl(tt.from);
      const tenants = TestBed.inject(TenantService);

      expect(await tenants.open(tt.tenant)).toBe(true);

      expect(TestBed.inject(Router).url).toBe(tt.want);
      expect(tenants.current()).toBe('');
    });
  }
});
