import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { inject, Injectable, signal } from '@angular/core';
import { Router } from '@angular/router';
import { firstValueFrom } from 'rxjs';

import { MatSnackBar } from '@angular/material/snack-bar';

import type { UserRole, UserStatus } from '@shared/types';
import { mapAuthSessionErrorMessage, TokenService } from '@web/shared';

@Injectable({ providedIn: 'root' })
export class AuthService {
  private readonly httpClient = inject(HttpClient);
  private readonly router = inject(Router);
  private readonly snackBar = inject(MatSnackBar);
  private readonly tokenService = inject(TokenService);

  private _isLoading = signal(false);
  isLoading = this._isLoading.asReadonly();

  login(username: string, password: string) {
    this._isLoading.set(true);

    this.httpClient.post<{ username: string }>('/api/auth/login', { username, password }).subscribe({
      next: () => {
        void firstValueFrom(this.tokenService.restoreSession()).then(() => {
          this._isLoading.set(false);
          this.router.navigate(['/'], { replaceUrl: true });
        });
      },
      error: (err) => {
        this._isLoading.set(false);
        this.openSnackBar(this.getAuthErrorMessage(err), 'OK');
      },
    });
  }

  signUp(username: string, email: string, password: string, avatar?: string) {
    this._isLoading.set(true);

    this.httpClient
      .post<{ id: string; username: string; email: string; role?: UserRole; status?: UserStatus; isAdmin?: boolean }>(
        '/api/user',
        {
          username,
          email,
          password,
          avatar,
        },
      )
      .subscribe({
        next: () => {
          this._isLoading.set(false);
          this.openSnackBar('Η εγγραφή ολοκληρώθηκε. Συνδεθείτε για να συνεχίσετε.', 'OK');
          this.router.navigate(['/auth/login'], { replaceUrl: true });
        },
        error: (err) => {
          this._isLoading.set(false);
          this.openSnackBar(this.getAuthErrorMessage(err), 'OK');
        },
      });
  }

  private getAuthErrorMessage(error: unknown): string {
    if (!(error instanceof HttpErrorResponse)) {
      return 'Προέκυψε σφάλμα πιστοποίησης. Δοκιμάστε ξανά.';
    }

    const status = error.status;

    if (status === 401 || status === 429) {
      return mapAuthSessionErrorMessage(error);
    }

    if (status === 404) {
      return 'Ο χρήστης δεν βρέθηκε.';
    }

    if (status === 403) {
      return 'Μη έγκυρα στοιχεία σύνδεσης.';
    }

    if (status === 409) {
      return 'Το email ή το όνομα χρήστη χρησιμοποιείται ήδη.';
    }

    return String(error.error?.message ?? 'Προέκυψε σφάλμα πιστοποίησης. Δοκιμάστε ξανά.');
  }

  private openSnackBar(message: string, action: string) {
    this.snackBar.open(message, action, { duration: 3000 });
  }
}
