import { ApiProperty } from '@nestjs/swagger';
import { IsEmail, IsNotEmpty } from 'class-validator';

export class CreateUserDto {
    @ApiProperty({ type: String, required: true })
    @IsNotEmpty()
    readonly username!: string;

    @ApiProperty({ type: String, required: true })
    @IsEmail()
    readonly email!: string;

    @ApiProperty({ type: String, required: true })
    @IsNotEmpty()
    readonly password!: string;

    @ApiProperty({ type: String })
    readonly avatar!: string;

    @ApiProperty({ type: Boolean, default: false })
    readonly isAdmin!: boolean;
}
