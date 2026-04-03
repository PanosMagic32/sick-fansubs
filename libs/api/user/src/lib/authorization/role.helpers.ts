import type { UserRole, UserStatus } from '@shared/types';

const roleRank: Record<UserRole, number> = {
  'super-admin': 4,
  admin: 3,
  moderator: 2,
  user: 1,
};

export interface RoleClaims {
  role?: UserRole;
  status?: UserStatus;
  isAdmin?: boolean;
}

export function resolveRole(claims: RoleClaims): UserRole {
  if (claims.role) {
    return claims.role;
  }

  return claims.isAdmin ? 'admin' : 'user';
}

export function resolveStatus(claims: RoleClaims): UserStatus {
  return claims.status ?? 'active';
}

export function isRoleAtLeast(role: UserRole, minimum: UserRole): boolean {
  return roleRank[role] >= roleRank[minimum];
}

export function isAdminLike(role: UserRole): boolean {
  return role === 'admin' || role === 'super-admin';
}
