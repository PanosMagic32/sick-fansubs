import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';

import { MaterialModule } from '@sick/material';

import { LoginComponent } from './feature/login/login.component';
import { SignupComponent } from './feature/signup/signup.component';
import { ClientAuthRoutingModule } from './client-auth-routing.module';
import { ReactiveFormsModule } from '@angular/forms';
import { LoginFormComponent } from './ui/login-form/login-form.component';
import { SignupFormComponent } from './ui/signup-form/signup-form.component';

@NgModule({
  imports: [CommonModule, ClientAuthRoutingModule, MaterialModule, ReactiveFormsModule],
  declarations: [LoginComponent, SignupComponent, LoginFormComponent, SignupFormComponent],
  exports: [LoginComponent, SignupComponent, LoginFormComponent, SignupFormComponent],
})
export class ClientAuthModule {}
