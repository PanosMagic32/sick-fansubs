import { Routes } from '@angular/router';

import { requireAuthGuard } from '@web/auth';

import { WebAccountComponent } from './feature/account.component';

export const accountRoutes: Routes = [{ path: '', canActivate: [requireAuthGuard], component: WebAccountComponent }];
