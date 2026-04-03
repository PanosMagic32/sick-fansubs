import { ForbiddenException } from '@nestjs/common';
import { vi } from 'vitest';

import { UserService } from './user.service';

describe('UserService', () => {
  let service: UserService;
  const userModelMock = {
    find: vi.fn(),
    findOne: vi.fn(),
    create: vi.fn(),
    db: {
      collection: vi.fn(),
    },
  };

  const configServiceMock = {
    get: vi.fn((key: string, fallback?: string) => fallback),
  };

  beforeEach(() => {
    vi.clearAllMocks();
    service = new UserService(userModelMock as never, configServiceMock as never);
  });

  it('should be defined', () => {
    expect(service).toBeDefined();
  });

  it('denies findAll for non-admin-like roles', async () => {
    await expect(
      service.findAll({ sub: 'u-1', role: 'moderator', status: 'active', isAdmin: false }),
    ).rejects.toBeInstanceOf(ForbiddenException);
  });

  it('allows findAll for admin-like roles and maps role/status', async () => {
    const mockDoc = {
      _id: { toString: () => 'u-1' },
      toObject: () => ({
        username: 'legacy-admin',
        email: 'legacy@example.com',
        avatar: 'https://example.com/a.png',
        isAdmin: true,
        favoriteBlogPostIds: [],
        createdBlogPostIds: [],
      }),
    };

    userModelMock.find.mockReturnValue({ exec: vi.fn().mockResolvedValue([mockDoc]) });

    const result = await service.findAll({ sub: 'admin-id', role: 'admin', status: 'active', isAdmin: true });

    expect(result[0]).toEqual(
      expect.objectContaining({
        id: 'u-1',
        role: 'admin',
        status: 'active',
        isAdmin: true,
      }),
    );
  });

  it('creates user with default role/status/isAdmin', async () => {
    userModelMock.findOne.mockResolvedValue(null);
    userModelMock.create.mockResolvedValue({
      _id: { toString: () => 'u-2' },
      username: 'new-user',
      email: 'new@example.com',
    });

    await service.create({
      username: 'new-user',
      email: 'new@example.com',
      password: 'Password1!',
      avatar: 'https://example.com/a.png',
    });

    expect(userModelMock.create).toHaveBeenCalledWith(
      expect.objectContaining({
        role: 'user',
        status: 'active',
        isAdmin: false,
      }),
    );
  });
});
