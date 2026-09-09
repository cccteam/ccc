import { Routes } from '@angular/router';
import { LoginAuthenticationGuard } from '@cccteam/resource-angular/auth-authentication-guard';

export const routes: Routes = [
  {
    path: 'login',
    loadComponent: () => import('./components/login/login.component').then((comp) => comp.LoginComponent),
  },
  {
    path: 'tracker',
    canActivate: [LoginAuthenticationGuard],
    loadComponent: () => import('./components/tracker/tracker.component').then((comp) => comp.TrackerComponent),
  },
  { path: '**', redirectTo: 'tracker' },
];
