import { inject } from '@angular/core';
import { type CanActivateFn, RedirectCommand, Router } from '@angular/router';

import { TokenService } from '@web/shared';

export const authGuard: CanActivateFn = () => {
  const tokenService = inject(TokenService);

  const token = tokenService.getToken();

  if (token && token !== '') {
    const tokenDecode = JSON.parse(atob(token.split('.')[1]));

    if (tokenDecode['_doc'].isAdmin && !tokenService._tokenExpired(tokenDecode.exp)) {
      return true;
    }
  }

  const urlTree = inject(Router).parseUrl('/auth/login');
  return new RedirectCommand(urlTree);
};
