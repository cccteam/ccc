import { Routes } from '@angular/router';
import { LoginAuthenticationGuard } from '@cccteam/resource-angular/auth-authentication-guard';
import { UiComponent } from '@components/ui/ui.component';

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
      {
        path: 'dashboard',
        loadComponent: () =>
          import('./components/ui/dashboard/dashboard.component').then((comp) => comp.DashboardComponent),
      },
      { path: '**', redirectTo: 'dashboard' },
    ],
  },
];
