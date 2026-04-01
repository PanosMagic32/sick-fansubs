import type { Routes } from '@angular/router';

import { adminGuard } from '@web/auth';

import { BlogPostListComponent } from './feature/blog-post-list/blog-post-list.component';

export const blogPostRoutes: Routes = [
  { path: '', component: BlogPostListComponent },
  {
    path: 'create',
    canActivate: [adminGuard],
    loadComponent: () => import('./feature/blog-post-create/blog-post-create.component'),
  },
  {
    path: ':id/edit',
    canActivate: [adminGuard],
    loadComponent: () => import('./feature/blog-post-edit/blog-post-edit.component'),
  },
];
