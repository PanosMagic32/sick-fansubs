import { HttpClient } from '@angular/common/http';
import { inject, Injectable, signal } from '@angular/core';
import { catchError, map, of, tap } from 'rxjs';

interface AuthSessionResponse {
  sub: string;
  username: string;
  email: string;
  isAdmin: boolean;
}

@Injectable({ providedIn: 'root' })
export class TokenService {
  private readonly httpClient = inject(HttpClient);

  private _isAdmin = signal(false);
  isAdmin = this._isAdmin.asReadonly();

  private _userId = signal<string | null>(null);
  userId = this._userId.asReadonly();

  private _isAuthenticated = signal(false);
  isAuthenticated = this._isAuthenticated.asReadonly();

  private _isInitialized = signal(false);
  isInitialized = this._isInitialized.asReadonly();

  restoreSession() {
    return this.httpClient.get<AuthSessionResponse>('/api/auth/session', { withCredentials: true }).pipe(
      tap((session) => {
        this.applySession(session);
      }),
      map(() => undefined),
      catchError(() => {
        this.removeToken();
        return of(undefined);
      }),
      tap(() => {
        this._isInitialized.set(true);
      }),
    );
  }

  logout() {
    return this.httpClient.post('/api/auth/logout', {}, { withCredentials: true }).pipe(
      tap(() => this.removeToken()),
      catchError(() => {
        this.removeToken();
        return of(undefined);
      }),
    );
  }

  private applySession(session: AuthSessionResponse) {
    this._isAdmin.set(Boolean(session.isAdmin));
    this._userId.set(session.sub || null);
    this._isAuthenticated.set(Boolean(session.sub));
  }

  removeToken() {
    this._isAdmin.set(false);
    this._userId.set(null);
    this._isAuthenticated.set(false);
    this._isInitialized.set(true);
  }

  isValidToken(): boolean {
    return this._isAuthenticated();
  }

  getUserId(): string | null {
    return this._userId();
  }
}
