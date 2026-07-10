import { UnauthorizedException } from '@nestjs/common';
import { vi } from 'vitest';

import { ApiAuthService } from './api-auth.service';

describe('ApiAuthService', () => {
  let service: ApiAuthService;

  const jwtServiceMock = {
    signAsync: vi.fn(),
    verifyAsync: vi.fn(),
  };

  const userServiceMock = {
    findOneByUsername: vi.fn(),
    storeRefreshTokenSession: vi.fn(),
    isRefreshTokenSessionValid: vi.fn(),
    clearRefreshTokenSession: vi.fn(),
    rotateRefreshTokenSession: vi.fn(),
  };

  const configServiceMock = {
    get: vi.fn((key: string) => {
      switch (key) {
        case 'JWT_SECRET':
          return 'access_secret';
        case 'JWT_EXPIRATION_TIME':
          return '900s';
        case 'JWT_REFRESH_SECRET':
          return 'refresh_secret';
        case 'JWT_REFRESH_EXPIRATION_TIME':
          return '7d';
        case 'NODE_ENV':
          return 'test';
        default:
          return undefined;
      }
    }),
  };

  beforeEach(async () => {
    vi.clearAllMocks();

    service = new ApiAuthService(jwtServiceMock as never, userServiceMock as never, configServiceMock as never);
  });

  it('should be defined', () => {
    expect(service).toBeDefined();
  });

  it('creates access and refresh tokens on login', async () => {
    vi.spyOn(service, 'comparePasswords').mockResolvedValue(true);

    userServiceMock.findOneByUsername.mockResolvedValue({
      _id: { toString: () => 'user-id-1' },
      username: 'tester',
      email: 'tester@example.com',
      role: 'moderator',
      status: 'active',
      password: 'ignored-in-test',
    });

    jwtServiceMock.signAsync.mockResolvedValueOnce('access-token-1').mockResolvedValueOnce('refresh-token-1');
    jwtServiceMock.verifyAsync.mockResolvedValueOnce({ exp: Math.floor(Date.now() / 1000) + 60 * 60 * 24 * 7 });

    const result = await service.login({ username: 'tester', password: 'test-password' });

    expect(result.username).toBe('tester');
    expect(result.accessToken).toBe('access-token-1');
    expect(result.refreshToken).toBe('refresh-token-1');
    expect(userServiceMock.storeRefreshTokenSession).toHaveBeenCalledTimes(1);
    expect(jwtServiceMock.signAsync).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({
        role: 'moderator',
        status: 'active',
      }),
      expect.any(Object),
    );
  });

  it('preserves super-admin role from refresh payload', async () => {
    jwtServiceMock.verifyAsync.mockResolvedValueOnce({
      sub: 'user-id-3',
      username: 'root',
      email: 'root@example.com',
      role: 'super-admin',
      status: 'active',
      jti: 'jti-root',
    });

    userServiceMock.rotateRefreshTokenSession.mockResolvedValue(undefined);
    jwtServiceMock.signAsync.mockResolvedValueOnce('access-token-root').mockResolvedValueOnce('refresh-token-root');
    jwtServiceMock.verifyAsync.mockResolvedValueOnce({ exp: Math.floor(Date.now() / 1000) + 60 * 60 * 24 * 7 });

    await service.refresh('refresh-token-root');

    expect(jwtServiceMock.signAsync).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({ role: 'super-admin', status: 'active' }),
      expect.any(Object),
    );
  });

  it('strips standard JWT claims (exp, iat) from the payload before signing new tokens', async () => {
    // Simulate a real decoded refresh token — it includes exp, iat, iss, aud
    // alongside custom claims.  These MUST NOT leak into the new signing payload.
    jwtServiceMock.verifyAsync.mockResolvedValueOnce({
      sub: 'user-id-2',
      username: 'editor',
      email: 'editor@example.com',
      role: 'admin',
      status: 'active',
      jti: 'jti-editor',
      exp: 1710000000,
      iat: 1700000000,
      iss: 'sick-fansubs',
      aud: 'sick-fansubs-api',
    });

    userServiceMock.rotateRefreshTokenSession.mockResolvedValue(undefined);
    jwtServiceMock.signAsync.mockResolvedValueOnce('access-token').mockResolvedValueOnce('refresh-token');
    jwtServiceMock.verifyAsync.mockResolvedValueOnce({ exp: Math.floor(Date.now() / 1000) + 60 * 60 * 24 * 7 });

    await service.refresh('refresh-token');

    // The signing payload must NOT contain exp, iat, iss, aud.
    expect(jwtServiceMock.signAsync).toHaveBeenNthCalledWith(
      1,
      expect.not.objectContaining({ exp: expect.anything(), iat: expect.anything() }),
      expect.any(Object),
    );
    expect(jwtServiceMock.signAsync).toHaveBeenNthCalledWith(
      2,
      expect.not.objectContaining({ exp: expect.anything(), iat: expect.anything() }),
      expect.any(Object),
    );

    // Core identity claims are preserved.
    expect(jwtServiceMock.signAsync).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({
        sub: 'user-id-2',
        username: 'editor',
        email: 'editor@example.com',
        role: 'admin',
        status: 'active',
      }),
      expect.any(Object),
    );
  });

  it('rejects refresh when refresh session is revoked', async () => {
    jwtServiceMock.verifyAsync.mockResolvedValue({
      sub: 'user-id-1',
      username: 'tester',
      email: 'tester@example.com',
      role: 'user',
      status: 'active',
      jti: 'jti-1',
    });
    userServiceMock.rotateRefreshTokenSession.mockRejectedValue(
      new UnauthorizedException('Refresh token session invalid or already used.'),
    );

    await expect(service.refresh('refresh-token-1')).rejects.toBeInstanceOf(UnauthorizedException);
  });
});
