import { ForbiddenException, Injectable, NotFoundException, UnauthorizedException } from '@nestjs/common';
import { ConfigService } from '@nestjs/config';
import { JwtService } from '@nestjs/jwt';
import * as bcrypt from 'bcrypt';
import { randomUUID } from 'crypto';
import type { CookieOptions } from 'express';

import type { UserRole, UserStatus } from '@shared/types';
import { LoginUserDto, UserService } from '@api/user';

import { JWT_AUDIENCE, JWT_ISSUER } from './auth.constants';
import type { AuthJwtPayload, RefreshTokenPayload } from './types/auth-session.types';

@Injectable()
export class ApiAuthService {
  private readonly accessTokenSecret: string;
  private readonly accessTokenExpiration: string;
  private readonly refreshTokenSecret: string;
  private readonly refreshTokenExpiration: string;

  constructor(
    private readonly jwtService: JwtService,
    private readonly userService: UserService,
    private readonly configService: ConfigService,
  ) {
    this.accessTokenSecret = this.configService.get<string>('JWT_SECRET') ?? 'dev_access_secret';
    this.accessTokenExpiration = this.configService.get<string>('JWT_EXPIRATION_TIME') ?? '15m';
    this.refreshTokenSecret = this.configService.get<string>('JWT_REFRESH_SECRET') ?? this.accessTokenSecret;
    this.refreshTokenExpiration = this.configService.get<string>('JWT_REFRESH_EXPIRATION_TIME') ?? '7d';
  }

  private toRoleAndStatus(payload: { role?: UserRole; status?: UserStatus; isAdmin?: boolean }): {
    role: UserRole;
    status: UserStatus;
    isAdmin: boolean;
  } {
    const role = payload.role ?? (payload.isAdmin ? 'admin' : 'user');
    const status = payload.status ?? 'active';
    return {
      role,
      status,
      isAdmin: role === 'admin' || role === 'super-admin',
    };
  }

  private async generateAccessToken(payload: AuthJwtPayload): Promise<string> {
    const identity = this.toRoleAndStatus(payload);
    return this.jwtService.signAsync(
      {
        ...payload,
        ...identity,
      },
      {
        secret: this.accessTokenSecret,
        expiresIn: Math.floor(this.parseToMilliseconds(this.accessTokenExpiration) / 1000),
        issuer: JWT_ISSUER,
        audience: JWT_AUDIENCE,
      },
    );
  }

  private async generateRefreshToken(payload: AuthJwtPayload): Promise<{ token: string; jti: string; expiresAt: Date }> {
    const identity = this.toRoleAndStatus(payload);
    const jti = randomUUID();
    const token = await this.jwtService.signAsync(
      {
        ...payload,
        ...identity,
        jti,
      },
      {
        secret: this.refreshTokenSecret,
        expiresIn: Math.floor(this.parseToMilliseconds(this.refreshTokenExpiration) / 1000),
        issuer: JWT_ISSUER,
        audience: JWT_AUDIENCE,
      },
    );

    const decoded = await this.jwtService.verifyAsync<{ exp?: number }>(token, {
      secret: this.refreshTokenSecret,
      issuer: JWT_ISSUER,
      audience: JWT_AUDIENCE,
    });

    if (!decoded.exp || decoded.exp <= 0) {
      throw new UnauthorizedException('Invalid refresh token expiration claim.');
    }

    return {
      token,
      jti,
      expiresAt: new Date(decoded.exp * 1000),
    };
  }

  async comparePasswords(password: string, storedPasswordHash: string): Promise<boolean> {
    return bcrypt.compare(password, storedPasswordHash);
  }

  private verifyRefreshToken(refreshToken: string): Promise<RefreshTokenPayload> {
    return this.jwtService.verifyAsync(refreshToken, {
      secret: this.refreshTokenSecret,
      issuer: JWT_ISSUER,
      audience: JWT_AUDIENCE,
    });
  }

  private parseToMilliseconds(duration: string): number {
    const durationText = duration.trim();
    const match = durationText.match(/^(\d+)(ms|s|m|h|d)?$/i);

    if (!match) return 0;

    const value = Number.parseInt(match[1], 10);
    const unit = (match[2] ?? 'ms').toLowerCase();

    switch (unit) {
      case 'ms':
        return value;
      case 's':
        return value * 1000;
      case 'm':
        return value * 60_000;
      case 'h':
        return value * 3_600_000;
      case 'd':
        return value * 86_400_000;
      default:
        return 0;
    }
  }

  private getBaseCookieOptions(): CookieOptions {
    const isProduction = this.configService.get<string>('NODE_ENV') === 'production';

    return {
      httpOnly: true,
      secure: isProduction,
      sameSite: 'lax',
      path: '/',
    };
  }

  getAccessTokenCookieOptions(): CookieOptions {
    const maxAge = this.parseToMilliseconds(this.accessTokenExpiration);

    return {
      ...this.getBaseCookieOptions(),
      ...(maxAge > 0 ? { maxAge } : {}),
    };
  }

  getRefreshTokenCookieOptions(): CookieOptions {
    const maxAge = this.parseToMilliseconds(this.refreshTokenExpiration);

    return {
      ...this.getBaseCookieOptions(),
      ...(maxAge > 0 ? { maxAge } : {}),
    };
  }

  getCookieClearOptions(): CookieOptions {
    return this.getBaseCookieOptions();
  }

  async validateUser(username: string, pass: string): Promise<AuthJwtPayload> {
    const user = await this.userService.findOneByUsername(username);
    const role = user.role ?? (user.isAdmin ? 'admin' : 'user');
    const status = user.status ?? 'active';

    if (user && (await this.comparePasswords(pass, user.password))) {
      return {
        sub: user._id.toString(),
        username: user.username,
        email: user.email,
        role,
        status,
        isAdmin: role === 'admin' || role === 'super-admin',
      };
    }

    if (!user) throw new NotFoundException('User not found.');

    throw new ForbiddenException('Invalid user credentials.');
  }

  private async createSession(payload: AuthJwtPayload) {
    const identity = this.toRoleAndStatus(payload);
    const sessionPayload = { ...payload, ...identity };
    const accessToken = await this.generateAccessToken(sessionPayload);
    const refreshTokenData = await this.generateRefreshToken(sessionPayload);

    await this.userService.storeRefreshTokenSession(
      sessionPayload.sub,
      refreshTokenData.token,
      refreshTokenData.jti,
      refreshTokenData.expiresAt,
    );

    return {
      username: sessionPayload.username,
      accessToken,
      refreshToken: refreshTokenData.token,
    };
  }

  async login(loginUserDto: LoginUserDto): Promise<{ username: string; accessToken: string; refreshToken: string }> {
    const payload = await this.validateUser(loginUserDto.username, loginUserDto.password);

    return this.createSession(payload);
  }

  async refresh(refreshToken: string): Promise<{ username: string; accessToken: string; refreshToken: string }> {
    if (!refreshToken) {
      throw new UnauthorizedException('Refresh token missing.');
    }

    let refreshPayload: RefreshTokenPayload;

    try {
      refreshPayload = await this.verifyRefreshToken(refreshToken);
    } catch {
      throw new UnauthorizedException('Refresh token invalid or expired.');
    }

    const isValidSession = await this.userService.isRefreshTokenSessionValid(
      refreshPayload.sub,
      refreshToken,
      refreshPayload.jti,
    );

    if (!isValidSession) {
      await this.userService.clearRefreshTokenSession(refreshPayload.sub);
      throw new UnauthorizedException('Refresh token revoked.');
    }

    return this.createSession({
      sub: refreshPayload.sub,
      username: refreshPayload.username,
      email: refreshPayload.email,
      role: refreshPayload.role,
      status: refreshPayload.status,
      isAdmin: refreshPayload.isAdmin,
    });
  }

  async logout(refreshToken: string): Promise<void> {
    if (!refreshToken) return;

    try {
      const refreshPayload = await this.verifyRefreshToken(refreshToken);
      await this.userService.clearRefreshTokenSession(refreshPayload.sub);
    } catch {
      // Do not leak refresh token validity details during logout.
    }
  }
}
