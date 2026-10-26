import { Route, Routes } from '@angular/router';
import { DomainRoutePath } from '@app/service/zz_gen_resources';
import { tenantGuard, tenantLeave } from '@app/tenant/tenant.guard';
import { refusedSegment } from '@app/tenant/tenant.service';
import { LoginAuthenticationGuard } from '@cccteam/resource-angular/auth-authentication-guard';
import { tenantSignInRoute } from '@components/ui/sign-in/sign-in.routes';
import { tokensRoute } from '@components/ui/tokens/tokens.routes';
import { UiComponent } from '@components/ui/ui.component';
import { tenantUsersRoute, usersRoute } from '@components/ui/users/users.routes';

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
    path: 'logout',
    loadComponent: () => import('@cccteam/resource-angular/auth-sign-out').then((comp) => comp.SignOutComponent),
  },
  {
    path: '',
    component: UiComponent,
    canActivate: [LoginAuthenticationGuard],
    children: [
      dashboardRoute(),
      // The Users page composes the library's user-management component over the global
      // partition of the auth's user surface, behind the List grant on its users resource.
      usersRoute(),
      // The Tokens page composes the library's tokens component over the descriptor's
      // tokens block: the signed-in person's own tokens for the machines outlet.
      tokensRoute(),
      // The device page, where a command-line client sends the person for approval
      // (/console/device?userCode=..., the staff auth's DevicePage); no navigation item.
      {
        path: 'device',
        loadComponent: () => import('./components/ui/device/device.component').then((comp) => comp.DeviceComponent),
      },
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
        children: [
          dashboardRoute(),
          // The tenant's partition of the user surface, with the route's tenant as its domain.
          tenantUsersRoute(),
          // Who may enter the tenant: its directories and whether they are the only way in.
          tenantSignInRoute(),
          { path: '**', redirectTo: 'dashboard' },
        ],
      },
      { path: '**', redirectTo: 'dashboard' },
    ],
  },
];
