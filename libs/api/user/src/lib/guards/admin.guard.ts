import { CanActivate, ExecutionContext, ForbiddenException, Injectable } from '@nestjs/common';
import type { UserRole } from '@shared/types';

import { hasAdminRole } from '../authorization/role.helpers';

@Injectable()
export class AdminGuard implements CanActivate {
  canActivate(context: ExecutionContext): boolean {
    const request = context.switchToHttp().getRequest<{ user?: { role?: UserRole } }>();
    const role = request.user?.role ?? 'user';

    if (!hasAdminRole(role)) {
      throw new ForbiddenException('Admin access required');
    }

    return true;
  }
}
