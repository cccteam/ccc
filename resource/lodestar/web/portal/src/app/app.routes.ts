import { Routes } from '@angular/router';
import { LoginAuthenticationGuard } from '@cccteam/ccc-lib/auth-authentication-guard';

export const routes: Routes = [
  {
    path: 'login',
    loadComponent: () => import('./components/login/login.component').then((comp) => comp.LoginComponent),
  },
  {
    path: '',
    canActivate: [LoginAuthenticationGuard],
    loadComponent: () => import('./components/tracker/tracker.component').then((comp) => comp.TrackerComponent),
  },
  { path: '**', redirectTo: '' },
];
