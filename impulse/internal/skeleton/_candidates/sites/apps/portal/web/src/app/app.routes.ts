import { Route, Routes } from '@angular/router';
import { DomainRoutePath } from '@app/service/zz_gen_resources';
import { tenantGuard, tenantLeave } from '@app/tenant/tenant.guard';
import { refusedSegment } from '@app/tenant/tenant.service';
import { LoginAuthenticationGuard } from '@cccteam/resource-angular/auth-authentication-guard';
import { UiComponent } from '@components/ui/ui.component';

// The dashboard lives in both places: outside a tenant it shows the global digest, under
// a tenant the digest there.
const dashboardRoute = (): Route => ({
  path: 'dashboard',
  loadComponent: () => import('./components/ui/dashboard/dashboard.component').then((comp) => comp.DashboardComponent),
});

export const routes: Routes = [
  {
    path: 'login',
    loadComponent: () => import('./components/login/login.component').then((comp) => comp.LoginComponent),
  },
  {
    path: '',
    component: UiComponent,
    canActivate: [LoginAuthenticationGuard],
    children: [
      dashboardRoute(),
      // Where a URL naming a tenant the session cannot reach lands (tenantGuard): the
      // server's answer, under the tenant's own URL. Declared ahead of the segment, so the
      // guard is not asked again on the way there.
      {
        path: `${DomainRoutePath}/${refusedSegment}`,
        loadComponent: () =>
          import('./components/ui/tenant-refused/tenant-refused.component').then((comp) => comp.TenantRefusedComponent),
      },
      // The tenant segment, tenants/:tenantID, mirroring the API's: every tenant-scoped
      // page lives under it, so a link to one can be shared and a bookmark lands where it
      // points. The guard settles the tenant from the URL before any page here loads and
      // clears it on the way out; nothing else writes it.
      {
        path: DomainRoutePath,
        canActivate: [tenantGuard],
        canDeactivate: [tenantLeave],
        children: [dashboardRoute(), { path: '**', redirectTo: 'dashboard' }],
      },
      { path: '**', redirectTo: 'dashboard' },
    ],
  },
];
