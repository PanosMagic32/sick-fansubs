import type { Routes } from '@angular/router';

import { adminGuard } from '@web/auth';

import { ProjectListComponent } from './feature/project-list/project-list.component';

export const projectsRoutes: Routes = [
  {
    path: '',
    component: ProjectListComponent,
  },
  {
    path: 'create',
    canActivate: [adminGuard],
    loadComponent: () => import('./feature/project-create/project-create.component'),
  },
  {
    path: ':id',
    loadComponent: () => import('./feature/project-details/project-details.component'),
  },
  {
    path: ':id/edit',
    canActivate: [adminGuard],
    loadComponent: () => import('./feature/project-edit/project-edit.component'),
  },
];
