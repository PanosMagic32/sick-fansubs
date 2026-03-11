import { ApiProperty } from '@nestjs/swagger';
import { IsString, Length, MaxLength, MinLength } from 'class-validator';

export class LoginUserDto {
  @ApiProperty({ type: String })
  @IsString()
  @Length(3, 32)
  readonly username!: string;

  @ApiProperty({ type: String })
  @IsString()
  @MinLength(8)
  @MaxLength(128)
  readonly password!: string;
}
