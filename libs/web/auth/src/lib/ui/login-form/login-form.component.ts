import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { FormControl, FormGroup, ReactiveFormsModule, Validators } from '@angular/forms';

import { MatButton } from '@angular/material/button';
import { MatCard } from '@angular/material/card';
import { MatError, MatFormField, MatLabel } from '@angular/material/form-field';
import { MatInput } from '@angular/material/input';
import { MatProgressBar } from '@angular/material/progress-bar';

import { AuthService } from '../../data-access/auth.service';
import type { LoginFormModel } from '../../data-access/login-form.interface';

@Component({
  selector: 'sf-login-form',
  templateUrl: './login-form.component.html',
  styleUrl: './login-form.component.scss',
  imports: [ReactiveFormsModule, MatCard, MatFormField, MatLabel, MatInput, MatError, MatProgressBar, MatButton],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class LoginFormComponent {
  private readonly authService = inject(AuthService);

  protected readonly isLoading = this.authService.isLoading;

  protected readonly loginForm = new FormGroup<LoginFormModel>({
    username: new FormControl('', {
      nonNullable: true,
      validators: [Validators.required],
    }),
    password: new FormControl('', {
      nonNullable: true,
      validators: [Validators.required, Validators.minLength(6)],
    }),
  });

  get formControl() {
    return this.loginForm.controls;
  }

  onLogin() {
    if (!this.loginForm.value.username || !this.loginForm.value.password) return;

    this.authService.login(this.loginForm.value.username, this.loginForm.value.password);
  }
}
