import { Routes } from '@angular/router';
import { LoginAuthenticationGuard } from '@cccteam/resource-angular/auth-authentication-guard';
import { UiComponent } from '@components/ui/ui.component';
import { usersRoute } from '@components/ui/users/users.routes';

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
      {
        path: 'dashboard',
        loadComponent: () =>
          import('./components/ui/dashboard/dashboard.component').then((comp) => comp.DashboardComponent),
      },
      // The Users page composes the library's user-management component over the auth's
      // user surface, behind the List grant on its users resource.
      usersRoute(),
      { path: '**', redirectTo: 'dashboard' },
    ],
  },
];
