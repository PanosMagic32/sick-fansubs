import { CanActivate, ExecutionContext, ForbiddenException, Injectable } from '@nestjs/common';
import type { UserRole } from '@shared/types';

import { isAdminLike, resolveRole } from '../authorization/role.helpers';

@Injectable()
export class AdminGuard implements CanActivate {
  canActivate(context: ExecutionContext): boolean {
    const request = context.switchToHttp().getRequest<{ user?: { role?: UserRole; isAdmin?: boolean } }>();
    const role = resolveRole({ role: request.user?.role, isAdmin: request.user?.isAdmin });

    if (!isAdminLike(role)) {
      throw new ForbiddenException('Admin access required');
    }

    return true;
  }
}
