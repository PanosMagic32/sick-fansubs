import { ApiProperty } from '@nestjs/swagger';
import { IsEmail, IsOptional, IsString, Matches, MaxLength, MinLength } from 'class-validator';

export class UpdateUserDto {
  @ApiProperty({ type: String, required: false })
  @IsOptional()
  @IsEmail()
  @MaxLength(254)
  readonly email?: string;

  @ApiProperty({ type: String, required: false })
  @IsOptional()
  @IsString()
  @MaxLength(512)
  readonly avatar?: string;

  @ApiProperty({ type: String, required: false, minLength: 8, maxLength: 128 })
  @IsOptional()
  @IsString()
  @MinLength(8)
  @MaxLength(128)
  @Matches(/[a-z]/)
  @Matches(/[A-Z]/)
  @Matches(/\d/)
  @Matches(/[^A-Za-z0-9]/)
  readonly password?: string;
}
