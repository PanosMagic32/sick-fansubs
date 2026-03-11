import { Injectable, signal } from '@angular/core';

const JWT_TOKEN = 'token';

@Injectable({ providedIn: 'root' })
export class TokenService {
  private _isAdmin = signal(false);
  isAdmin = this._isAdmin.asReadonly();

  private _userId = signal<string | null>(null);
  userId = this._userId.asReadonly();

  constructor() {
    this.getUserIDFromToken();
  }

  private decodeToken(): Record<string, unknown> | null {
    const token = this.getToken();

    if (!token) {
      return null;
    }

    try {
      const tokenPayload = token.split('.')[1];
      if (!tokenPayload) {
        return null;
      }

      return JSON.parse(atob(tokenPayload)) as Record<string, unknown>;
    } catch {
      return null;
    }
  }

  setToken(token: string) {
    localStorage.setItem(JWT_TOKEN, token);
    this.getUserIDFromToken();
  }

  getToken(): string {
    const token = localStorage.getItem(JWT_TOKEN);
    if (token) return token;
    return '';
  }

  removeToken() {
    localStorage.removeItem(JWT_TOKEN);
    this._isAdmin.set(false);
    this._userId.set(null);
  }

  isValidToken(): boolean {
    const tokenDecode = this.decodeToken();

    if (tokenDecode && tokenDecode['exp']) {
      return !this._tokenExpired(tokenDecode['exp'] as string | number);
    }

    return false;
  }

  getUserIDFromToken() {
    const tokenDecode = this.decodeToken();

    if (!tokenDecode) {
      this._isAdmin.set(false);
      this._userId.set(null);
      return;
    }

    const nestedDoc = tokenDecode['_doc'] as Record<string, unknown> | undefined;
    const isAdmin = Boolean(tokenDecode['isAdmin'] ?? nestedDoc?.['isAdmin'] ?? false);
    const userId = (tokenDecode['sub'] as string | undefined) ?? (nestedDoc?.['_id'] as string | undefined) ?? null;

    this._isAdmin.set(isAdmin);
    this._userId.set(userId);
  }

  _tokenExpired(expiration: string | number): boolean {
    return Math.floor(new Date().getTime() / 1000) >= +expiration;
  }

  getUserId(): string | null {
    return this._userId();
  }
}
