import { Injectable } from '@angular/core';
import { Router, UrlTree } from '@angular/router';
import { Observable } from 'rxjs';

import { TokenService } from './token.service';

@Injectable({
  providedIn: 'root',
})
export class LoggedInGuard {
  constructor(
    private readonly router: Router,
    private readonly tokenService: TokenService,
  ) {}

  canActivate(): boolean | UrlTree | Observable<boolean | UrlTree> | Promise<boolean | UrlTree> {
    const token = this.tokenService.getToken();

    if (token) {
      const tokenDecode = JSON.parse(atob(token.split('.')[1]));

      if (tokenDecode['_doc'].isAdmin && !this._tokenExpired(tokenDecode.exp)) {
        this.router.navigate(['/']);
        return false;
      }
    }

    return true;
  }

  private _tokenExpired(expiration: string | number): boolean {
    return Math.floor(new Date().getTime() / 1000) >= +expiration;
  }
}
