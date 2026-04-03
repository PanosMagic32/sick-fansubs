import type { UserRole, UserStatus } from '@shared/types';

export interface AuthJwtPayload {
  sub: string;
  username: string;
  email: string;
  role?: UserRole;
  status?: UserStatus;
  isAdmin?: boolean;
}

export interface RefreshTokenPayload extends AuthJwtPayload {
  jti: string;
}

export interface AuthSessionPayload {
  sub: string;
  username: string;
  email: string;
  role: UserRole;
  status: UserStatus;
  isAdmin: boolean;
}
