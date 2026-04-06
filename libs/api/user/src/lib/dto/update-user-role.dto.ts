import { ApiProperty } from '@nestjs/swagger';
import { IsIn } from 'class-validator';

import type { UserRole } from '@shared/types';

export class UpdateUserRoleDto {
  @ApiProperty({
    type: String,
    enum: ['super-admin', 'admin', 'moderator', 'user'],
    description: 'The new role for the user',
  })
  @IsIn(['super-admin', 'admin', 'moderator', 'user'])
  readonly role!: UserRole;
}
