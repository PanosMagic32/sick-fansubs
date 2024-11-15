import { HttpClient } from '@angular/common/http';
import { inject, Injectable, signal } from '@angular/core';
import { Router } from '@angular/router';

import { MatSnackBar } from '@angular/material/snack-bar';

import { TokenService } from '@web/shared';

@Injectable({
  providedIn: 'root',
})
export class AuthService {
  private readonly httpClient = inject(HttpClient);
  private readonly router = inject(Router);
  private readonly snackBar = inject(MatSnackBar);
  private readonly tokenService = inject(TokenService);

  private _isLoading = signal(false);
  isLoading = this._isLoading.asReadonly();

  login(username: string, password: string) {
    this.httpClient.post<{ username: string; accessToken: string }>('/api/auth/login', { username, password }).subscribe({
      next: (res) => {
        this._isLoading.set(false);

        this.tokenService.setToken(res.accessToken);
        this.router.navigate(['/'], { replaceUrl: true });
      },
      error: (err) => {
        this._isLoading.set(false);
        this.openSnackBar(err.error.message, 'OK');
      },
    });
  }

  signUp(username: string, email: string, password: string, avatar?: string) {
    this.httpClient
      .post<{ id: string; username: string; email: string; isAdmin: boolean }>('/api/user', {
        username,
        email,
        password,
        avatar,
      })
      .subscribe({
        next: (res) => {
          this.saveUser(res);
          this.router.navigate(['/'], { replaceUrl: true });
        },
        error: (err) => {
          this._isLoading.set(false);
          this.openSnackBar(err.error.message, 'OK');
        },
      });
  }

  private saveUser(res: { id: string; username: string; email: string }) {
    localStorage.setItem('USER', JSON.stringify(res));
  }

  private openSnackBar(message: string, action: string) {
    this.snackBar.open(message, action, { duration: 3000 });
  }
}
