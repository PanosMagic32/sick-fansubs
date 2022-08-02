import { ApiProperty, PartialType } from '@nestjs/swagger';
import { IsEmail, IsNotEmpty } from 'class-validator';

import { CreateUserDto } from './create-user.dto';

export class UpdateUserDto extends PartialType(CreateUserDto) {
  @ApiProperty()
  @IsNotEmpty()
  override readonly username!: string;

  @ApiProperty()
  @IsEmail()
  override readonly email!: string;

  @ApiProperty()
  override readonly avatar!: string;
}
