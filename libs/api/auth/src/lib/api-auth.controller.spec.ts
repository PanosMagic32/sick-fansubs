import { ApiAuthController } from './api-auth.controller';
import { vi } from 'vitest';

describe('ApiAuthController', () => {
  let controller: ApiAuthController;
  const apiAuthServiceMock = {
    login: vi.fn(),
    refresh: vi.fn(),
    logout: vi.fn(),
    getAccessTokenCookieOptions: vi.fn(),
    getRefreshTokenCookieOptions: vi.fn(),
    getCookieClearOptions: vi.fn(),
  };

  beforeEach(() => {
    vi.clearAllMocks();
    controller = new ApiAuthController(apiAuthServiceMock as never);
  });

  it('should be defined', () => {
    expect(controller).toBeTruthy();
  });

  it('maps session from explicit role/status payload', async () => {
    const result = await controller.session({
      user: {
        sub: 'u-1',
        username: 'mod',
        email: 'mod@example.com',
        role: 'moderator',
        status: 'active',
      },
    });

    expect(result).toEqual({
      sub: 'u-1',
      username: 'mod',
      email: 'mod@example.com',
      role: 'moderator',
      status: 'active',
    });
  });
});
