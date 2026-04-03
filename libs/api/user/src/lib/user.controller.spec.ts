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
    addFavoriteBlogPost: vi.fn(),
    removeFavoriteBlogPost: vi.fn(),
    update: vi.fn(),
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
});
