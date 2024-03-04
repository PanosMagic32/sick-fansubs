import { NgModule } from '@angular/core';
import { RouterModule, Routes } from '@angular/router';

import { LoginComponent } from './feature/login/login.component';
import { SignupComponent } from './feature/signup/signup.component';
import { LoggedInGuard } from './data-access/logged-in.guard';

const routes: Routes = [
  {
    path: '',
    redirectTo: 'login',
    pathMatch: 'full',
  },
  {
    path: 'login',
    canActivate: [LoggedInGuard],
    component: LoginComponent,
  },
  {
    path: 'signup',
    canActivate: [LoggedInGuard],
    component: SignupComponent,
  },
];

@NgModule({
  imports: [RouterModule.forChild(routes)],
  exports: [RouterModule],
})
export class ClientAuthRoutingModule {}
