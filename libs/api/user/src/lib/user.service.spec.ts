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

  it('denies findManagementUsers for non-admin-like roles', async () => {
    await expect(
      service.findManagementUsers({ sub: 'u-1', role: 'moderator', status: 'active', isAdmin: false }, {}),
    ).rejects.toBeInstanceOf(ForbiddenException);
  });

  it('returns paginated management users for admin-like roles', async () => {
    const mockDoc = {
      _id: { toString: () => 'u-1' },
      toObject: () => ({
        username: 'alpha',
        email: 'alpha@example.com',
        role: 'user',
        status: 'active',
        favoriteBlogPostIds: [],
        createdBlogPostIds: [],
      }),
    };

    const findExec = vi.fn().mockResolvedValue([mockDoc]);
    const findLimit = vi.fn().mockReturnValue({ exec: findExec });
    const findSkip = vi.fn().mockReturnValue({ limit: findLimit });
    const findSort = vi.fn().mockReturnValue({ skip: findSkip });
    const findMock = vi.fn().mockReturnValue({ sort: findSort });
    const countExec = vi.fn().mockResolvedValue(1);
    const countDocumentsMock = vi.fn().mockReturnValue({ exec: countExec });

    userModelMock.find = findMock;
    userModelMock.countDocuments = countDocumentsMock;

    const result = await service.findManagementUsers(
      { sub: 'admin-id', role: 'admin', status: 'active', isAdmin: true },
      { page: 2, pageSize: 20, search: 'alpha', sortBy: 'username', sortDirection: 'asc' },
    );

    expect(result).toEqual(
      expect.objectContaining({
        count: 1,
        page: 2,
        pageSize: 20,
      }),
    );
    expect(result.users[0]).toEqual(expect.objectContaining({ id: 'u-1', username: 'alpha' }));
    expect(countDocumentsMock).toHaveBeenCalled();
    expect(findMock).toHaveBeenCalled();
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
