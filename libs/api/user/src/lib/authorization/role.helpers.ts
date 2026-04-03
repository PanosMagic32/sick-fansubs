import type { UserRole, UserStatus } from '@shared/types';

export function resolveStatus(status?: UserStatus): UserStatus {
  return status ?? 'active';
}

export function hasAdminRole(role: UserRole): boolean {
  return role === 'admin' || role === 'super-admin';
}
