import { Component } from '@angular/core';
import { AbstractControl, FormControl, FormGroup, ValidatorFn, Validators } from '@angular/forms';
import { MatSnackBar } from '@angular/material/snack-bar';

import { AuthService } from '../../data-access/auth.service';
import { SignupForm } from '../../data-access/signup-form.interface';

@Component({
  selector: 'sick-signup-form',
  templateUrl: './signup-form.component.html',
  styleUrls: ['./signup-form.component.scss'],
})
export class SignupFormComponent {
  isLoading = false;

  signupForm = new FormGroup<SignupForm>(
    {
      username: new FormControl('', { nonNullable: true, validators: [Validators.required] }),
      email: new FormControl('', { nonNullable: true, validators: [Validators.required, Validators.email] }),
      password: new FormControl('', { nonNullable: true, validators: [Validators.required, Validators.minLength(6)] }),
      confirmPassword: new FormControl('', {
        nonNullable: true,
        validators: [Validators.required, Validators.minLength(6)],
      }),
      // avatar: new FormControl(''),
    },
    { validators: this.checkPasswords('password', 'confirmPassword') }
  );

  get formControl() {
    return this.signupForm.controls;
  }

  constructor(private authService: AuthService, private snackBar: MatSnackBar) {}

  onSignup() {
    if (
      !this.signupForm.value.username ||
      !this.signupForm.value.email ||
      !this.signupForm.value.password ||
      !this.signupForm.value.confirmPassword
    ) {
      return;
    }

    this.isLoading = true;

    this.authService
      .signUp(this.signupForm.value.username, this.signupForm.value.email, this.signupForm.value.password)
      .subscribe({
        next: (res) => {
          console.log(res);
          this.isLoading = false;
          // TODO - handle successfull response
        },
        error: (err) => {
          this.isLoading = false;
          this.openSnackBar(err.error.message, 'OK');
        },
      });
  }

  private openSnackBar(message: string, action: string) {
    this.snackBar.open(message, action, { duration: 3000 });
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
