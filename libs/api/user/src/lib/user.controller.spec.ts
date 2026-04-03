import { UserController } from './user.controller';
import { vi } from 'vitest';

describe('UserController', () => {
  let controller: UserController;
  const userServiceMock = {
    create: vi.fn(),
    findAll: vi.fn(),
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

  it('maps legacy admin actor when role is missing', async () => {
    userServiceMock.findAll.mockResolvedValue([]);

    await controller.findAll({ user: { sub: 'u-1', isAdmin: true } });

    expect(userServiceMock.findAll).toHaveBeenCalledWith({
      sub: 'u-1',
      role: 'admin',
      status: 'active',
      isAdmin: true,
    });
  });

  it('maps explicit role actor and derives non-admin correctly', async () => {
    userServiceMock.findAll.mockResolvedValue([]);

    await controller.findAll({ user: { sub: 'u-2', role: 'moderator', status: 'active' } });

    expect(userServiceMock.findAll).toHaveBeenCalledWith({
      sub: 'u-2',
      role: 'moderator',
      status: 'active',
      isAdmin: false,
    });
  });
});
