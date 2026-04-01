import { inject } from '@angular/core';
import { type CanActivateFn, RedirectCommand, Router } from '@angular/router';

import { TokenService } from '@web/shared';

export const adminGuard: CanActivateFn = () => {
  const tokenService = inject(TokenService);

  if (tokenService.isAuthenticated() && tokenService.isAdmin()) {
    return true;
  }

  const urlTree = inject(Router).parseUrl('/auth/login');
  return new RedirectCommand(urlTree);
};
