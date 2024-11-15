import type { Routes } from '@angular/router';

import { authGuard } from '@web/auth';

import { BlogPostListComponent } from './feature/blog-post-list/blog-post-list.component';

export const blogPostRoutes: Routes = [
  { path: '', component: BlogPostListComponent },
  {
    path: 'create',
    canActivate: [authGuard],
    loadComponent: () => import('./feature/blog-post-create/blog-post-create.component'),
  },
  {
    path: ':id/edit',
    canActivate: [authGuard],
    loadComponent: () => import('./feature/blog-post-edit/blog-post-edit.component'),
  },
];
