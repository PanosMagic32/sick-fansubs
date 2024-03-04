import { Injectable } from '@angular/core';
import { BehaviorSubject } from 'rxjs';

const JWT_TOKEN = 'token';

@Injectable({
  providedIn: 'root',
})
export class TokenService {
  private isAdmin = new BehaviorSubject(false);

  get isAdmin$() {
    return this.isAdmin.asObservable();
  }

  setToken(token: string) {
    localStorage.setItem(JWT_TOKEN, token);
    this.getUserIDFromToken();
  }

  getToken(): string {
    const token = localStorage.getItem(JWT_TOKEN);

    if (token) {
      return token;
    } else {
      return '';
    }
  }

  removeToken() {
    localStorage.removeItem(JWT_TOKEN);
    this.isAdmin.next(false);
  }

  isValidToken(): boolean {
    const token = this.getToken();

    if (token && token !== '') {
      const tokenDecode = JSON.parse(atob(token.split('.')[1]));

      return !this._tokenExpired(tokenDecode.exp);
    } else {
      return false;
    }
  }

  getUserIDFromToken() {
    const token = this.getToken();

    if (token && token !== '') {
      const tokenDecode = JSON.parse(atob(token.split('.')[1]));

      if (tokenDecode['_doc'].isAdmin) {
        this.isAdmin.next(true);
      } else {
        this.isAdmin.next(false);
      }
    } else {
      this.isAdmin.next(false);
    }
  }

  private _tokenExpired(expiration: string | number): boolean {
    return Math.floor(new Date().getTime() / 1000) >= +expiration;
  }
}
