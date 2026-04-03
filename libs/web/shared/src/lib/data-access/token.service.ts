import { HttpClient } from '@angular/common/http';
import { computed, inject, Injectable, signal } from '@angular/core';
import { catchError, map, of, tap } from 'rxjs';

import type { UserRole, UserStatus } from '@shared/types';

interface AuthSessionResponse {
  sub: string;
  username: string;
  email: string;
  role?: UserRole;
  status?: UserStatus;
  isAdmin?: boolean;
}

@Injectable({ providedIn: 'root' })
export class TokenService {
  private readonly httpClient = inject(HttpClient);

  private _isAdmin = signal(false);
  isAdmin = this._isAdmin.asReadonly();

  private _role = signal<UserRole>('user');
  role = this._role.asReadonly();

  private _status = signal<UserStatus>('active');
  status = this._status.asReadonly();

  isStaff = computed(() => this._role() !== 'user');
  isAdminLike = computed(() => this._role() === 'admin' || this._role() === 'super-admin');

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
    const role = session.role ?? (session.isAdmin ? 'admin' : 'user');
    const status = session.status ?? 'active';

    this._role.set(role);
    this._status.set(status);
    this._isAdmin.set(role === 'admin' || role === 'super-admin');
    this._userId.set(session.sub || null);
    this._isAuthenticated.set(Boolean(session.sub));
  }

  removeToken() {
    this._isAdmin.set(false);
    this._role.set('user');
    this._status.set('active');
    this._userId.set(null);
    this._isAuthenticated.set(false);
    this._isInitialized.set(true);
  }

  isValidToken(): boolean {
    return this._isAuthenticated();
  }

  hasRole(role: UserRole): boolean {
    return this._role() === role;
  }

  hasAnyRole(roles: UserRole[]): boolean {
    return roles.includes(this._role());
  }

  canAccessDashboard(): boolean {
    return this.isAuthenticated() && this.hasAnyRole(['super-admin', 'admin', 'moderator']);
  }

  getUserId(): string | null {
    return this._userId();
  }
}
