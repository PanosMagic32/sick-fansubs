import type { Routes } from '@angular/router';

export const appRoutes: Routes = [
  {
    path: '',
    loadChildren: () => import('@web/blog-post').then((r) => r.blogPostRoutes),
  },
  {
    path: 'projects',
    loadChildren: () => import('@web/projects').then((r) => r.projectsRoutes),
  },
  {
    path: 'about',
    loadChildren: () => import('@web/about').then((r) => r.aboutRoutes),
  },
  {
    path: 'auth',
    loadChildren: () => import('@web/auth').then((r) => r.authRoutes),
  },
  {
    path: 'search',
    loadChildren: () => import('@web/search').then((r) => r.searchRoutes),
  },
  {
    path: '**',
    redirectTo: '',
    pathMatch: 'full',
  },
];
