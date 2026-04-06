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

  it('sorts favorite posts on the API before pagination', async () => {
    const userDoc = {
      _id: { toString: () => 'u-1' },
      toObject: () => ({
        username: 'viewer',
        email: 'viewer@example.com',
        role: 'user',
        status: 'active',
        favoriteBlogPostIds: ['507f191e810c19729de860ea', '507f191e810c19729de860eb', '507f191e810c19729de860ec'],
        createdBlogPostIds: [],
      }),
    };

    const toArray = vi.fn().mockResolvedValue([
      {
        _id: { toString: () => '507f191e810c19729de860eb' },
        title: 'B',
        subtitle: '',
        description: 'Second',
        thumbnail: '',
        downloadLink: '',
        downloadLinkTorrent: '',
        dateTimeCreated: '2025-02-01T00:00:00.000Z',
      },
    ]);
    const limit = vi.fn().mockReturnValue({ toArray });
    const skip = vi.fn().mockReturnValue({ limit });
    const sort = vi.fn().mockReturnValue({ skip });
    const find = vi.fn().mockReturnValue({ sort });

    userModelMock.db.collection.mockReturnValue({ find });
    (service.findOneEntityById as never) = vi.fn().mockResolvedValue(userDoc);

    const result = await service.getFavoriteBlogPosts('u-1', { sub: 'u-1', role: 'user', status: 'active' }, 1, 2, 'newest');

    const query = find.mock.calls[0]?.[0] as { _id?: { $in?: unknown[] } };

    expect(find).toHaveBeenCalledWith({
      _id: {
        $in: expect.any(Array),
      },
    });
    expect(query._id?.$in).toHaveLength(3);
    expect(sort).toHaveBeenCalledWith({ dateTimeCreated: -1, _id: -1 });
    expect(skip).toHaveBeenCalledWith(1);
    expect(limit).toHaveBeenCalledWith(1);
    expect(result).toEqual({
      count: 3,
      posts: [expect.objectContaining({ _id: '507f191e810c19729de860eb', title: 'B' })],
    });
  });

  it('supports oldest-first favorite sorting on the API', async () => {
    const userDoc = {
      _id: { toString: () => 'u-2' },
      toObject: () => ({
        username: 'viewer',
        email: 'viewer@example.com',
        role: 'user',
        status: 'active',
        favoriteBlogPostIds: ['507f191e810c19729de860ed'],
        createdBlogPostIds: [],
      }),
    };

    const toArray = vi.fn().mockResolvedValue([]);
    const limit = vi.fn().mockReturnValue({ toArray });
    const skip = vi.fn().mockReturnValue({ limit });
    const sort = vi.fn().mockReturnValue({ skip });
    const find = vi.fn().mockReturnValue({ sort });

    userModelMock.db.collection.mockReturnValue({ find });
    (service.findOneEntityById as never) = vi.fn().mockResolvedValue(userDoc);

    await service.getFavoriteBlogPosts('u-2', { sub: 'u-2', role: 'user', status: 'active' }, 10, 1, 'oldest');

    expect(sort).toHaveBeenCalledWith({ dateTimeCreated: 1, _id: 1 });
  });

  it('sorts favorite projects on the API before pagination', async () => {
    const userDoc = {
      _id: { toString: () => 'u-10' },
      toObject: () => ({
        username: 'viewer',
        email: 'viewer@example.com',
        role: 'user',
        status: 'active',
        favoriteProjectIds: ['507f191e810c19729de860fa', '507f191e810c19729de860fb'],
        favoriteBlogPostIds: [],
        createdBlogPostIds: [],
      }),
    };

    const toArray = vi.fn().mockResolvedValue([
      {
        _id: { toString: () => '507f191e810c19729de860fb' },
        title: 'Project B',
        description: 'Second project',
        thumbnail: '',
        dateTimeCreated: '2025-03-01T00:00:00.000Z',
        batchDownloadLinks: [],
      },
    ]);
    const limit = vi.fn().mockReturnValue({ toArray });
    const skip = vi.fn().mockReturnValue({ limit });
    const sort = vi.fn().mockReturnValue({ skip });
    const find = vi.fn().mockReturnValue({ sort });

    userModelMock.db.collection.mockReturnValue({ find });
    (service.findOneEntityById as never) = vi.fn().mockResolvedValue(userDoc);

    const result = await service.getFavoriteProjects(
      'u-10',
      { sub: 'u-10', role: 'user', status: 'active' },
      1,
      2,
      'newest',
    );

    expect(find).toHaveBeenCalledWith({
      _id: {
        $in: expect.any(Array),
      },
    });
    expect(sort).toHaveBeenCalledWith({ dateTimeCreated: -1, _id: -1 });
    expect(skip).toHaveBeenCalledWith(1);
    expect(limit).toHaveBeenCalledWith(1);
    expect(result).toEqual({
      count: 2,
      projects: [expect.objectContaining({ _id: '507f191e810c19729de860fb', title: 'Project B' })],
    });
  });

  it('supports oldest-first project favorites sorting on the API', async () => {
    const userDoc = {
      _id: { toString: () => 'u-11' },
      toObject: () => ({
        username: 'viewer',
        email: 'viewer@example.com',
        role: 'user',
        status: 'active',
        favoriteProjectIds: ['507f191e810c19729de860fc'],
        favoriteBlogPostIds: [],
        createdBlogPostIds: [],
      }),
    };

    const toArray = vi.fn().mockResolvedValue([]);
    const limit = vi.fn().mockReturnValue({ toArray });
    const skip = vi.fn().mockReturnValue({ limit });
    const sort = vi.fn().mockReturnValue({ skip });
    const find = vi.fn().mockReturnValue({ sort });

    userModelMock.db.collection.mockReturnValue({ find });
    (service.findOneEntityById as never) = vi.fn().mockResolvedValue(userDoc);

    await service.getFavoriteProjects('u-11', { sub: 'u-11', role: 'user', status: 'active' }, 10, 1, 'oldest');

    expect(sort).toHaveBeenCalledWith({ dateTimeCreated: 1, _id: 1 });
  });

  it('denies admin from promoting a user to super-admin', async () => {
    const targetUser = { _id: 'u-7', role: 'user' };
    (service.findOneEntityById as never) = vi.fn().mockResolvedValue(targetUser);

    await expect(
      service.updateUserRole('507f191e810c19729de860aa', 'super-admin', { sub: 'a-1', role: 'admin', status: 'active' }),
    ).rejects.toThrow('Only super-admins can promote to super-admin.');
  });

  it('denies admin from changing super-admin status', async () => {
    const targetUser = { _id: 'u-1', role: 'super-admin' };
    (service.findOneEntityById as never) = vi.fn().mockResolvedValue(targetUser);

    await expect(
      service.updateUserStatus('507f191e810c19729de860ab', 'suspended', { sub: 'a-1', role: 'admin', status: 'active' }),
    ).rejects.toThrow('Only super-admins can change super-admin status.');
  });

  describe('assertCanModifyUser - Authorization Tiers', () => {
    beforeEach(() => {
      // Reset findOneEntityById to return a mock user with role property
      vi.spyOn(service, 'findOneEntityById' as never);
    });

    it('allows super-admin to modify any user', async () => {
      const targetUser = { _id: 'u-2', role: 'admin' };
      (service.findOneEntityById as never) = vi.fn().mockResolvedValue(targetUser);

      await expect(
        service.assertCanModifyUser('u-2', { sub: 'admin-1', role: 'super-admin', status: 'active' }),
      ).resolves.not.toThrow();
    });

    it('allows super-admin to modify moderator', async () => {
      const targetUser = { _id: 'u-3', role: 'moderator' };
      (service.findOneEntityById as never) = vi.fn().mockResolvedValue(targetUser);

      await expect(
        service.assertCanModifyUser('u-3', { sub: 'admin-1', role: 'super-admin', status: 'active' }),
      ).resolves.not.toThrow();
    });

    it('allows super-admin to modify user', async () => {
      const targetUser = { _id: 'u-4', role: 'user' };
      (service.findOneEntityById as never) = vi.fn().mockResolvedValue(targetUser);

      await expect(
        service.assertCanModifyUser('u-4', { sub: 'admin-1', role: 'super-admin', status: 'active' }),
      ).resolves.not.toThrow();
    });

    it('allows admin to modify moderator', async () => {
      const targetUser = { _id: 'u-3', role: 'moderator' };
      (service.findOneEntityById as never) = vi.fn().mockResolvedValue(targetUser);

      await expect(
        service.assertCanModifyUser('u-3', { sub: 'admin-1', role: 'admin', status: 'active' }),
      ).resolves.not.toThrow();
    });

    it('allows admin to modify user', async () => {
      const targetUser = { _id: 'u-4', role: 'user' };
      (service.findOneEntityById as never) = vi.fn().mockResolvedValue(targetUser);

      await expect(
        service.assertCanModifyUser('u-4', { sub: 'admin-1', role: 'admin', status: 'active' }),
      ).resolves.not.toThrow();
    });

    it('denies admin modifying super-admin', async () => {
      const targetUser = { _id: 'u-1', role: 'super-admin' };
      (service.findOneEntityById as never) = vi.fn().mockResolvedValue(targetUser);

      await expect(service.assertCanModifyUser('u-1', { sub: 'admin-1', role: 'admin', status: 'active' })).rejects.toThrow(
        'Only super-admins can modify super-admin users.',
      );
    });

    it('allows moderator to modify user', async () => {
      const targetUser = { _id: 'u-4', role: 'user' };
      (service.findOneEntityById as never) = vi.fn().mockResolvedValue(targetUser);

      await expect(
        service.assertCanModifyUser('u-4', { sub: 'mod-1', role: 'moderator', status: 'active' }),
      ).resolves.not.toThrow();
    });

    it('allows moderator to modify another moderator', async () => {
      const targetUser = { _id: 'u-3', role: 'moderator' };
      (service.findOneEntityById as never) = vi.fn().mockResolvedValue(targetUser);

      await expect(
        service.assertCanModifyUser('u-3', { sub: 'mod-1', role: 'moderator', status: 'active' }),
      ).resolves.not.toThrow();
    });

    it('denies moderator modifying admin', async () => {
      const targetUser = { _id: 'u-2', role: 'admin' };
      (service.findOneEntityById as never) = vi.fn().mockResolvedValue(targetUser);

      await expect(
        service.assertCanModifyUser('u-2', { sub: 'mod-1', role: 'moderator', status: 'active' }),
      ).rejects.toThrow('Moderators can only modify other moderators and users.');
    });

    it('allows user to modify self', async () => {
      const targetUser = { _id: 'u-4', role: 'user' };
      (service.findOneEntityById as never) = vi.fn().mockResolvedValue(targetUser);

      await expect(
        service.assertCanModifyUser('u-4', { sub: 'u-4', role: 'user', status: 'active' }),
      ).resolves.not.toThrow();
    });

    it('denies user modifying another user', async () => {
      const targetUser = { _id: 'u-5', role: 'user' };
      (service.findOneEntityById as never) = vi.fn().mockResolvedValue(targetUser);

      await expect(service.assertCanModifyUser('u-5', { sub: 'u-4', role: 'user', status: 'active' })).rejects.toThrow(
        'You are not allowed to modify this user.',
      );
    });

    it('denies user modifying moderator', async () => {
      const targetUser = { _id: 'u-3', role: 'moderator' };
      (service.findOneEntityById as never) = vi.fn().mockResolvedValue(targetUser);

      await expect(service.assertCanModifyUser('u-3', { sub: 'u-4', role: 'user', status: 'active' })).rejects.toThrow(
        'You are not allowed to modify this user.',
      );
    });
  });

  describe('assertCanDeleteUser - Authorization Tiers', () => {
    beforeEach(() => {
      vi.spyOn(service, 'findOneEntityById' as never);
    });

    it('allows super-admin to delete any user', async () => {
      const targetUser = { _id: 'u-2', role: 'user' };
      (service.findOneEntityById as never) = vi.fn().mockResolvedValue(targetUser);

      await expect(
        service.assertCanDeleteUser('u-2', { sub: 'admin-1', role: 'super-admin', status: 'active' }),
      ).resolves.not.toThrow();
    });

    it('allows admin to delete user', async () => {
      const targetUser = { _id: 'u-4', role: 'user' };
      (service.findOneEntityById as never) = vi.fn().mockResolvedValue(targetUser);

      await expect(
        service.assertCanDeleteUser('u-4', { sub: 'admin-1', role: 'admin', status: 'active' }),
      ).resolves.not.toThrow();
    });

    it('denies admin deleting super-admin', async () => {
      const targetUser = { _id: 'u-1', role: 'super-admin' };
      (service.findOneEntityById as never) = vi.fn().mockResolvedValue(targetUser);

      await expect(service.assertCanDeleteUser('u-1', { sub: 'admin-1', role: 'admin', status: 'active' })).rejects.toThrow(
        'Only super-admins can delete super-admin users.',
      );
    });

    it('allows moderator to delete user', async () => {
      const targetUser = { _id: 'u-4', role: 'user' };
      (service.findOneEntityById as never) = vi.fn().mockResolvedValue(targetUser);

      await expect(
        service.assertCanDeleteUser('u-4', { sub: 'mod-1', role: 'moderator', status: 'active' }),
      ).resolves.not.toThrow();
    });

    it('allows moderator to delete another moderator', async () => {
      const targetUser = { _id: 'u-3', role: 'moderator' };
      (service.findOneEntityById as never) = vi.fn().mockResolvedValue(targetUser);

      await expect(
        service.assertCanDeleteUser('u-3', { sub: 'mod-1', role: 'moderator', status: 'active' }),
      ).resolves.not.toThrow();
    });

    it('allows user to delete self', async () => {
      const targetUser = { _id: 'u-4', role: 'user' };
      (service.findOneEntityById as never) = vi.fn().mockResolvedValue(targetUser);

      await expect(
        service.assertCanDeleteUser('u-4', { sub: 'u-4', role: 'user', status: 'active' }),
      ).resolves.not.toThrow();
    });

    it('denies user deleting another user', async () => {
      const targetUser = { _id: 'u-5', role: 'user' };
      (service.findOneEntityById as never) = vi.fn().mockResolvedValue(targetUser);

      await expect(service.assertCanDeleteUser('u-5', { sub: 'u-4', role: 'user', status: 'active' })).rejects.toThrow(
        'You are not allowed to delete this user.',
      );
    });
  });
});
