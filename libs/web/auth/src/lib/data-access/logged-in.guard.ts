import { inject } from '@angular/core';
import { type CanActivateFn, RedirectCommand, Router } from '@angular/router';

import { TokenService } from '@web/shared';

export const isLoggedInGuard: CanActivateFn = () => {
  const tokenService = inject(TokenService);

  if (tokenService.isAuthenticated()) {
    const urlTree = inject(Router).parseUrl('/');
    return new RedirectCommand(urlTree);
  }

  return true;
};
