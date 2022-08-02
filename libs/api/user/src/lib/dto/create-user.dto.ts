import { ApiProperty } from '@nestjs/swagger';
import { IsEmail, IsNotEmpty } from 'class-validator';

export class CreateUserDto {
  @ApiProperty()
  @IsNotEmpty()
  readonly username!: string;

  @ApiProperty()
  @IsEmail()
  readonly email!: string;

  @ApiProperty()
  @IsNotEmpty()
  readonly password!: string;

  @ApiProperty()
  readonly avatar!: string;
}
