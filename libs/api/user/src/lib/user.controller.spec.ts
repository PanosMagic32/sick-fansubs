import { UserController } from './user.controller';
import { vi } from 'vitest';

describe('UserController', () => {
  let controller: UserController;
  const userServiceMock = {
    create: vi.fn(),
    findAll: vi.fn(),
    findManagementUsers: vi.fn(),
    findOne: vi.fn(),
    getFavoriteBlogPostIds: vi.fn(),
    getFavoriteBlogPosts: vi.fn(),
    getFavoriteProjectIds: vi.fn(),
    getFavoriteProjects: vi.fn(),
    addFavoriteBlogPost: vi.fn(),
    removeFavoriteBlogPost: vi.fn(),
    addFavoriteProject: vi.fn(),
    removeFavoriteProject: vi.fn(),
    update: vi.fn(),
    updateUserRole: vi.fn(),
    updateUserStatus: vi.fn(),
    remove: vi.fn(),
  };

  beforeEach(() => {
    vi.clearAllMocks();
    controller = new UserController(userServiceMock as never);
  });

  it('should be defined', () => {
    expect(controller).toBeDefined();
  });

  it('maps actor defaults when role/status are missing', async () => {
    userServiceMock.findAll.mockResolvedValue([]);

    await controller.findAll({ user: { sub: 'u-1' } });

    expect(userServiceMock.findAll).toHaveBeenCalledWith({
      sub: 'u-1',
      role: 'user',
      status: 'active',
    });
  });

  it('maps explicit role actor and derives non-admin correctly', async () => {
    userServiceMock.findAll.mockResolvedValue([]);

    await controller.findAll({ user: { sub: 'u-2', role: 'moderator', status: 'active' } });

    expect(userServiceMock.findAll).toHaveBeenCalledWith({
      sub: 'u-2',
      role: 'moderator',
      status: 'active',
    });
  });

  it('maps management query params and actor correctly', async () => {
    userServiceMock.findManagementUsers.mockResolvedValue({ users: [], count: 0, page: 1, pageSize: 10 });

    await controller.findManagementUsers(
      { user: { sub: 'u-3', role: 'admin', status: 'active' } },
      '2',
      '20',
      'john',
      'moderator',
      'suspended',
      'username',
      'asc',
    );

    expect(userServiceMock.findManagementUsers).toHaveBeenCalledWith(
      {
        sub: 'u-3',
        role: 'admin',
        status: 'active',
      },
      {
        page: 2,
        pageSize: 20,
        search: 'john',
        role: 'moderator',
        status: 'suspended',
        sortBy: 'username',
        sortDirection: 'asc',
      },
    );
  });

  it('maps favorite posts pagination and sort params correctly', async () => {
    userServiceMock.getFavoriteBlogPosts.mockResolvedValue({ posts: [], count: 0 });

    await controller.getFavoritePosts('u-9', { user: { sub: 'u-9', role: 'user', status: 'active' } }, '24', '3', 'oldest');

    expect(userServiceMock.getFavoriteBlogPosts).toHaveBeenCalledWith(
      'u-9',
      {
        sub: 'u-9',
        role: 'user',
        status: 'active',
      },
      24,
      3,
      'oldest',
    );
  });

  it('maps favorite projects pagination and sort params correctly', async () => {
    userServiceMock.getFavoriteProjects.mockResolvedValue({ projects: [], count: 0 });

    await controller.getFavoriteProjects(
      'u-7',
      { user: { sub: 'u-7', role: 'user', status: 'active' } },
      '12',
      '2',
      'newest',
    );

    expect(userServiceMock.getFavoriteProjects).toHaveBeenCalledWith(
      'u-7',
      {
        sub: 'u-7',
        role: 'user',
        status: 'active',
      },
      12,
      2,
      'newest',
    );
  });

  it('defaults favorite post sort to newest when query sort is invalid', async () => {
    userServiceMock.getFavoriteBlogPosts.mockResolvedValue({ posts: [], count: 0 });

    await controller.getFavoritePosts(
      'u-11',
      { user: { sub: 'u-11', role: 'user', status: 'active' } },
      '10',
      '1',
      'invalid' as never,
    );

    expect(userServiceMock.getFavoriteBlogPosts).toHaveBeenCalledWith(
      'u-11',
      {
        sub: 'u-11',
        role: 'user',
        status: 'active',
      },
      10,
      1,
      'newest',
    );
  });

  it('maps role update payload and actor to user service', async () => {
    userServiceMock.updateUserRole.mockResolvedValue({ id: 'u-8', role: 'moderator' });

    await controller.updateRole(
      'u-8',
      { role: 'moderator' },
      { user: { sub: 'sa-1', role: 'super-admin', status: 'active' } },
    );

    expect(userServiceMock.updateUserRole).toHaveBeenCalledWith('u-8', 'moderator', {
      sub: 'sa-1',
      role: 'super-admin',
      status: 'active',
    });
  });

  it('maps status update payload and actor to user service', async () => {
    userServiceMock.updateUserStatus.mockResolvedValue({ id: 'u-8', status: 'suspended' });

    await controller.updateStatus('u-8', { status: 'suspended' }, { user: { sub: 'a-1', role: 'admin', status: 'active' } });

    expect(userServiceMock.updateUserStatus).toHaveBeenCalledWith('u-8', 'suspended', {
      sub: 'a-1',
      role: 'admin',
      status: 'active',
    });
  });
});
