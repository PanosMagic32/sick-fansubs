import type { Route } from '@angular/router';

import { authGuard } from '@web/auth';

import { ProjectListComponent } from './feature/project-list/project-list.component';

export const projectsRoutes: Route[] = [
  {
    path: '',
    component: ProjectListComponent,
  },
  {
    path: 'create',
    canActivate: [authGuard],
    loadComponent: () => import('./feature/project-create/project-create.component'),
  },
  {
    path: ':id',
    loadComponent: () => import('./feature/project-details/project-details.component'),
  },
  {
    path: ':id/edit',
    canActivate: [authGuard],
    loadComponent: () => import('./feature/project-edit/project-edit.component'),
  },
];
