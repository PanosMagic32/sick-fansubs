import { ApiProperty } from '@nestjs/swagger';
import { IsEmail, IsOptional, IsString, Length, Matches, MaxLength, MinLength } from 'class-validator';

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
  @Matches(/[a-z]/)
  @Matches(/[A-Z]/)
  @Matches(/\d/)
  @Matches(/[^A-Za-z0-9]/)
  readonly password!: string;

  @ApiProperty({ type: String, required: false })
  @IsOptional()
  @IsString()
  @MaxLength(512)
  readonly avatar?: string;
}
