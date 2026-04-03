import { Routes } from '@angular/router';

import { dashboardGuard } from './data-access/dashboard.guard';
import { DashboardNavigationComponent } from './feature/dashboard-navigation/dashboard-navigation.component';

export const dashboardRoutes: Routes = [
  {
    path: '',
    canActivate: [dashboardGuard],
    component: DashboardNavigationComponent,
    children: [
      {
        path: '',
        pathMatch: 'full',
        loadChildren: () => import('./feature/dashboard-home/dashboard-home.routes').then((m) => m.dashboardHomeRoutes),
      },
      {
        path: 'users',
        loadChildren: () => import('./feature/dashboard-users/dashboard-users.routes').then((m) => m.dashboardUsersRoutes),
      },
      {
        path: '**',
        redirectTo: '',
      },
    ],
  },
];
