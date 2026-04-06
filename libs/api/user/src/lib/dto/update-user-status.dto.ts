import { ApiProperty } from '@nestjs/swagger';
import { IsIn } from 'class-validator';

import type { UserStatus } from '@shared/types';

export class UpdateUserStatusDto {
  @ApiProperty({
    type: String,
    enum: ['active', 'suspended'],
    description: 'The new status for the user',
  })
  @IsIn(['active', 'suspended'])
  readonly status!: UserStatus;
}
