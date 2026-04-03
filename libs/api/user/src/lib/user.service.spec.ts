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

  it('allows findAll for moderator role', async () => {
    userModelMock.find.mockReturnValue({ exec: vi.fn().mockResolvedValue([]) });

    await expect(service.findAll({ sub: 'u-1', role: 'moderator', status: 'active' })).resolves.toEqual([]);
  });

  it('allows findAll for admin-like roles and maps role/status', async () => {
    const mockDoc = {
      _id: { toString: () => 'u-1' },
      toObject: () => ({
        username: 'legacy-admin',
        email: 'legacy@example.com',
        avatar: 'https://example.com/a.png',
        role: 'admin',
        status: 'active',
        favoriteBlogPostIds: [],
        createdBlogPostIds: [],
      }),
    };

    userModelMock.find.mockReturnValue({ exec: vi.fn().mockResolvedValue([mockDoc]) });

    const result = await service.findAll({ sub: 'admin-id', role: 'admin', status: 'active' });

    expect(result[0]).toEqual(
      expect.objectContaining({
        id: 'u-1',
        role: 'admin',
        status: 'active',
      }),
    );
  });

  it('allows findManagementUsers for moderator role', async () => {
    const findExec = vi.fn().mockResolvedValue([]);
    const findLimit = vi.fn().mockReturnValue({ exec: findExec });
    const findSkip = vi.fn().mockReturnValue({ limit: findLimit });
    const findSort = vi.fn().mockReturnValue({ skip: findSkip });
    const findMock = vi.fn().mockReturnValue({ sort: findSort });
    const countExec = vi.fn().mockResolvedValue(0);
    const countDocumentsMock = vi.fn().mockReturnValue({ exec: countExec });

    userModelMock.find = findMock;
    userModelMock.countDocuments = countDocumentsMock;

    await expect(service.findManagementUsers({ sub: 'u-1', role: 'moderator', status: 'active' }, {})).resolves.toEqual(
      expect.objectContaining({
        users: [],
        count: 0,
        page: 1,
        pageSize: 10,
      }),
    );
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
      { sub: 'admin-id', role: 'admin', status: 'active' },
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

  it('creates user with default role/status', async () => {
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
      }),
    );
  });
});
