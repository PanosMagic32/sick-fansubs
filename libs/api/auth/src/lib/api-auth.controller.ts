import { Controller, Post, Body } from '@nestjs/common';

import { User } from '@sick/api/user';
import { ApiAuthService } from './api-auth.service';

@Controller('auth')
export class ApiAuthController {
  constructor(private readonly apiAuthService: ApiAuthService) {}

  @Post('login')
  async login(@Body() user: User): Promise<{ username: string; access_token: string }> {
    return this.apiAuthService.login(user);
  }
}
