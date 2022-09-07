import { ApiProperty, PartialType } from '@nestjs/swagger';
import { IsEmail, IsNotEmpty } from 'class-validator';

import { CreateUserDto } from './create-user.dto';

export class UpdateUserDto extends PartialType(CreateUserDto) {
  @ApiProperty({ type: String, required: true })
  @IsNotEmpty()
  override readonly username!: string;

  @ApiProperty({ type: String, required: true })
  @IsEmail()
  override readonly email!: string;

  @ApiProperty({ type: String })
  override readonly avatar!: string;

  @ApiProperty({ type: Boolean, default: false })
  override readonly isAdmin!: boolean;
}
