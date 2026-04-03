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

    userServiceMock.isRefreshTokenSessionValid.mockResolvedValue(true);
    jwtServiceMock.signAsync.mockResolvedValueOnce('access-token-root').mockResolvedValueOnce('refresh-token-root');
    jwtServiceMock.verifyAsync.mockResolvedValueOnce({ exp: Math.floor(Date.now() / 1000) + 60 * 60 * 24 * 7 });

    await service.refresh('refresh-token-root');

    expect(jwtServiceMock.signAsync).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({ role: 'super-admin', status: 'active' }),
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
    userServiceMock.isRefreshTokenSessionValid.mockResolvedValue(false);

    await expect(service.refresh('refresh-token-1')).rejects.toBeInstanceOf(UnauthorizedException);
    expect(userServiceMock.clearRefreshTokenSession).toHaveBeenCalledWith('user-id-1');
  });
});
