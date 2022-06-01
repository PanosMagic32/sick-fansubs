import { PartialType } from '@nestjs/swagger';
import { CreateUserDto } from './create-user.dto';

export class UpdateUserDto extends PartialType(CreateUserDto) {
  readonly username!: string;
  readonly email!: string;
  readonly avatar!: string;
}
