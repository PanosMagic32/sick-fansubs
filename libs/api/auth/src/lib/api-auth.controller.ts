import { Body, Controller, Get, Post, Req, Res, UseGuards } from '@nestjs/common';
import { ApiTags } from '@nestjs/swagger';
import type { Request, Response } from 'express';

import { CredentialThrottlerGuard, JwtAuthGuard, LoginUserDto } from '@api/user';
import { Throttle } from '@nestjs/throttler';

import { ApiAuthService } from './api-auth.service';
import { ACCESS_TOKEN_COOKIE, REFRESH_TOKEN_COOKIE } from './auth.constants';

@ApiTags('Auth')
@Controller('auth')
export class ApiAuthController {
  constructor(private readonly apiAuthService: ApiAuthService) {}

  @Post('login')
  @UseGuards(CredentialThrottlerGuard)
  @Throttle({ default: { limit: 5, ttl: 60000 } })
  async login(@Body() loginUserDto: LoginUserDto, @Res({ passthrough: true }) res: Response): Promise<{ username: string }> {
    const session = await this.apiAuthService.login(loginUserDto);

    res.cookie(ACCESS_TOKEN_COOKIE, session.accessToken, this.apiAuthService.getAccessTokenCookieOptions());
    res.cookie(REFRESH_TOKEN_COOKIE, session.refreshToken, this.apiAuthService.getRefreshTokenCookieOptions());

    return {
      username: session.username,
    };
  }

  @Post('refresh')
  @UseGuards(CredentialThrottlerGuard)
  @Throttle({ default: { limit: 10, ttl: 60000 } })
  async refresh(@Req() req: Request, @Res({ passthrough: true }) res: Response): Promise<{ refreshed: true }> {
    const refreshToken = req.cookies?.[REFRESH_TOKEN_COOKIE] as string | undefined;
    const session = await this.apiAuthService.refresh(refreshToken ?? '');

    res.cookie(ACCESS_TOKEN_COOKIE, session.accessToken, this.apiAuthService.getAccessTokenCookieOptions());
    res.cookie(REFRESH_TOKEN_COOKIE, session.refreshToken, this.apiAuthService.getRefreshTokenCookieOptions());

    return { refreshed: true };
  }

  @Post('logout')
  @UseGuards(CredentialThrottlerGuard)
  @Throttle({ default: { limit: 10, ttl: 60000 } })
  async logout(@Req() req: Request, @Res({ passthrough: true }) res: Response): Promise<{ loggedOut: true }> {
    const refreshToken = req.cookies?.[REFRESH_TOKEN_COOKIE] as string | undefined;
    await this.apiAuthService.logout(refreshToken ?? '');

    res.clearCookie(ACCESS_TOKEN_COOKIE, this.apiAuthService.getCookieClearOptions());
    res.clearCookie(REFRESH_TOKEN_COOKIE, this.apiAuthService.getCookieClearOptions());

    return { loggedOut: true };
  }

  @Get('session')
  @UseGuards(JwtAuthGuard)
  async session(@Req() req: { user?: { sub?: string; username?: string; email?: string; isAdmin?: boolean } }) {
    return {
      sub: req.user?.sub ?? '',
      username: req.user?.username ?? '',
      email: req.user?.email ?? '',
      isAdmin: Boolean(req.user?.isAdmin),
    };
  }
}
