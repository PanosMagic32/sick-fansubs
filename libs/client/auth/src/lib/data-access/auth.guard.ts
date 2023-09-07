import { Injectable } from '@angular/core';
import { Router, UrlTree } from '@angular/router';
import { Observable } from 'rxjs';

import { TokenService } from './token.service';

@Injectable({
  providedIn: 'root',
})
export class AuthGuard {
  constructor(
    private router: Router,
    private tokenService: TokenService,
  ) {}

  canActivate(): boolean | UrlTree | Observable<boolean | UrlTree> | Promise<boolean | UrlTree> {
    const token = this.tokenService.getToken();

    if (token && token !== '') {
      const tokenDecode = JSON.parse(atob(token.split('.')[1]));

      if (tokenDecode.isAdmin && !this._tokenExpired(tokenDecode.exp)) {
        return true;
      }
    }

    this.router.navigate(['/login']);
    return false;
  }

  private _tokenExpired(expiration: string | number): boolean {
    return Math.floor(new Date().getTime() / 1000) >= +expiration;
  }
}
