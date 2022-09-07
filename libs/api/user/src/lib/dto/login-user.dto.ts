import { ApiProperty } from '@nestjs/swagger';
import { IsNotEmpty } from 'class-validator';

export class LoginUserDto {
  @ApiProperty({ type: String })
  @IsNotEmpty()
  readonly username!: string;

  @ApiProperty({ type: String })
  @IsNotEmpty()
  readonly password!: string;
}
