import { inject } from '@angular/core';
import { type CanActivateFn, RedirectCommand, Router } from '@angular/router';

import { TokenService } from '@web/shared';

export const requireAuthGuard: CanActivateFn = () => {
  const tokenService = inject(TokenService);

  if (tokenService.isAuthenticated()) {
    return true;
  }

  const urlTree = inject(Router).parseUrl('/auth/login');
  return new RedirectCommand(urlTree);
};
