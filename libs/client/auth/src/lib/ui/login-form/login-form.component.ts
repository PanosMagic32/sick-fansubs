import { Component } from '@angular/core';
import { FormControl, FormGroup, Validators } from '@angular/forms';
import { Router } from '@angular/router';
import { MatSnackBar } from '@angular/material/snack-bar';

import { AuthService } from '../../data-access/auth.service';
import { LoginForm } from '../../data-access/login-form.interface';
import { TokenService } from '../../data-access/token.service';

@Component({
  selector: 'sick-login-form',
  templateUrl: './login-form.component.html',
  styleUrls: ['./login-form.component.scss'],
})
export class LoginFormComponent {
  isLoading = false;

  loginForm = new FormGroup<LoginForm>({
    username: new FormControl('', { nonNullable: true, validators: [Validators.required] }),
    password: new FormControl('', { nonNullable: true, validators: [Validators.required, Validators.minLength(6)] }),
  });

  get formControl() {
    return this.loginForm.controls;
  }

  constructor(
    private readonly authService: AuthService,
    private readonly snackBar: MatSnackBar,
    private readonly tokenService: TokenService,
    private readonly router: Router,
  ) {}

  onLogin() {
    if (!this.loginForm.value.username || !this.loginForm.value.password) {
      return;
    }

    this.isLoading = true;

    this.authService.login(this.loginForm.value.username, this.loginForm.value.password).subscribe({
      next: (res) => {
        this.isLoading = false;

        this.tokenService.setToken(res.accessToken);
        this.router.navigate(['/'], { replaceUrl: true });
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
}
