import { Injectable, signal } from '@angular/core';

const JWT_TOKEN = 'token';

@Injectable({ providedIn: 'root' })
export class TokenService {
  private _isAdmin = signal(false);
  isAdmin = this._isAdmin.asReadonly();

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
  }

  isValidToken(): boolean {
    const token = this.getToken();

    if (token && token !== '') {
      const tokenDecode = JSON.parse(atob(token.split('.')[1]));

      return !this._tokenExpired(tokenDecode.exp);
    }

    return false;
  }

  getUserIDFromToken() {
    const token = this.getToken();

    if (token && token !== '') {
      const tokenDecode = JSON.parse(atob(token.split('.')[1]));

      if (tokenDecode['_doc'].isAdmin) {
        this._isAdmin.set(true);
      } else {
        this._isAdmin.set(false);
      }
    } else {
      this._isAdmin.set(false);
    }
  }

  _tokenExpired(expiration: string | number): boolean {
    return Math.floor(new Date().getTime() / 1000) >= +expiration;
  }
}
