import type { Routes } from '@angular/router';

import { isLoggedInGuard } from './data-access/logged-in.guard';

export const authRoutes: Routes = [
  {
    path: '',
    redirectTo: 'login',
    pathMatch: 'full',
  },
  {
    path: 'login',
    canActivate: [isLoggedInGuard],
    loadComponent: () => import('./feature/login/login.component'),
  },
  {
    path: 'signup',
    canActivate: [isLoggedInGuard],
    loadComponent: () => import('./feature/signup/signup.component'),
  },
];
