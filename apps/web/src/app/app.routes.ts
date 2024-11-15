import type { Routes } from '@angular/router';

export const appRoutes: Routes = [
  {
    path: '',
    loadChildren: () => import('@web/blog-post').then((m) => m.blogPostRoutes),
  },
  {
    path: 'projects',
    loadChildren: () => import('@web/projects').then((m) => m.projectsRoutes),
  },
  {
    path: 'about',
    loadChildren: () => import('@web/about').then((m) => m.aboutRoutes),
  },
  {
    path: 'auth',
    loadChildren: () => import('@web/auth').then((m) => m.authRoutes),
  },
  {
    path: '**',
    redirectTo: '',
    pathMatch: 'full',
  },
];
