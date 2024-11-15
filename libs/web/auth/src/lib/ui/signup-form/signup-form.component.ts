import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import {
  type AbstractControl,
  FormControl,
  FormGroup,
  ReactiveFormsModule,
  type ValidatorFn,
  Validators,
} from '@angular/forms';

import { MatButton } from '@angular/material/button';
import { MatCard } from '@angular/material/card';
import { MatError, MatFormField, MatLabel } from '@angular/material/form-field';
import { MatInput } from '@angular/material/input';
import { MatProgressBar } from '@angular/material/progress-bar';

import { AuthService } from '../../data-access/auth.service';
import type { SignupFormModel } from '../../data-access/signup-form.interface';

@Component({
  selector: 'sf-signup-form',
  templateUrl: './signup-form.component.html',
  styleUrl: './signup-form.component.scss',
  standalone: true,
  imports: [ReactiveFormsModule, MatCard, MatFormField, MatLabel, MatInput, MatError, MatButton, MatProgressBar],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SignupFormComponent {
  private readonly authService = inject(AuthService);

  protected readonly isLoading = this.authService.isLoading;

  protected readonly signupForm = new FormGroup<SignupFormModel>(
    {
      username: new FormControl('', {
        nonNullable: true,
        validators: [Validators.required],
      }),
      email: new FormControl('', {
        nonNullable: true,
        validators: [Validators.required, Validators.email],
      }),
      password: new FormControl('', {
        nonNullable: true,
        validators: [Validators.required, Validators.minLength(6)],
      }),
      confirmPassword: new FormControl('', {
        nonNullable: true,
        validators: [Validators.required, Validators.minLength(6)],
      }),
      // avatar: new FormControl(''),
    },
    { validators: this.checkPasswords('password', 'confirmPassword') },
  );

  get formControl() {
    return this.signupForm.controls;
  }

  onSignup() {
    if (
      !this.signupForm.value.username ||
      !this.signupForm.value.email ||
      !this.signupForm.value.password ||
      !this.signupForm.value.confirmPassword
    )
      return;

    this.authService.signUp(this.signupForm.value.username, this.signupForm.value.email, this.signupForm.value.password);
  }

  private checkPasswords(password: string, confirmPassword: string): ValidatorFn {
    return (formGroup: AbstractControl): { [key: string]: boolean } | null => {
      const passwordControl = formGroup.get(password);
      const confirmPasswordControl = formGroup.get(confirmPassword);

      if (!passwordControl || !confirmPasswordControl) {
        return null;
      }

      if (confirmPasswordControl.errors && !confirmPasswordControl.errors['passwordMismatch']) {
        return null;
      }

      if (passwordControl.value !== confirmPasswordControl.value) {
        confirmPasswordControl.setErrors({ passwordMismatch: true });
        return { passwordMismatch: true };
      } else {
        confirmPasswordControl.setErrors(null);
        return null;
      }
    };
  }
}
