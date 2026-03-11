import { ApiProperty } from '@nestjs/swagger';
import { IsEmail, IsOptional, IsString, Length, MaxLength, MinLength } from 'class-validator';

export class CreateUserDto {
  @ApiProperty({ type: String, required: true })
  @IsString()
  @Length(3, 32)
  readonly username!: string;

  @ApiProperty({ type: String, required: true })
  @IsEmail()
  @MaxLength(254)
  readonly email!: string;

  @ApiProperty({ type: String, required: true })
  @IsString()
  @MinLength(8)
  @MaxLength(128)
  readonly password!: string;

  @ApiProperty({ type: String, required: false })
  @IsOptional()
  @IsString()
  @MaxLength(512)
  readonly avatar?: string;
}
