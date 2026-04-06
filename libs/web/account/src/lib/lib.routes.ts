import { Routes } from '@angular/router';

import { requireAuthGuard } from '@web/auth';

import { WebAccountComponent } from './feature/account/account.component';
import { unsavedChangesGuard } from './guards/unsaved-changes.guard';

export const accountRoutes: Routes = [
  {
    path: '',
    canActivate: [requireAuthGuard],
    canDeactivate: [unsavedChangesGuard],
    component: WebAccountComponent,
  },
];
