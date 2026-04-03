import type { UserRole, UserStatus } from '@shared/types';

export interface AuthActor {
  sub: string;
  role: UserRole;
  status: UserStatus;
}
