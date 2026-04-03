import { Routes } from '@angular/router';

import { dashboardGuard } from './data-access/dashboard.guard';
import { DashboardShellComponent } from './feature/dashboard-shell/dashboard-shell.component';

export const dashboardRoutes: Routes = [
  {
    path: '',
    canActivate: [dashboardGuard],
    component: DashboardShellComponent,
  },
];
