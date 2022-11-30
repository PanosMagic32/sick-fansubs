import { Controller, Post, Body } from '@nestjs/common';
import { ApiTags } from '@nestjs/swagger';

import { LoginUserDto } from '@sick/api/user';

import { ApiAuthService } from './api-auth.service';

@ApiTags('Auth')
@Controller('auth')
export class ApiAuthController {
    constructor(private readonly apiAuthService: ApiAuthService) {}

    @Post('login')
    async login(@Body() loginUserDto: LoginUserDto): Promise<{ username: string; accessToken: string }> {
        return this.apiAuthService.login(loginUserDto);
    }
}
