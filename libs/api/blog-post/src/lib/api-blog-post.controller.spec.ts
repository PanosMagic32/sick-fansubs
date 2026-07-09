import { vi } from 'vitest';

// ---------------------------------------------------------------------------
// Prevent module-level decorator errors by mocking modules that use
// class-validator / @nestjs/mongoose @Prop without explicit `type`.
// ---------------------------------------------------------------------------
vi.mock('../blog-post/src/lib/schemas/blog-post.schema', () => ({
  BlogPost: class BlogPost {},
  BlogPostDocument: class BlogPostDocument {},
}));

vi.mock('@api/shared', () => ({
  PaginationDto: class PaginationDto {
    page = 0;
    pagesize = 10;
  },
  ParseMongoIdPipe: class ParseMongoIdPipe {},
}));

vi.mock('@api/media', () => ({
  MediaService: class MediaService {},
}));

// ---------------------------------------------------------------------------
import { UnauthorizedException } from '@nestjs/common';

import { ApiBlogPostController } from './api-blog-post.controller';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const apiBlogPostServiceMock = {
  create: vi.fn(),
  findAll: vi.fn(),
  findOne: vi.fn(),
  update: vi.fn(),
  delete: vi.fn(),
};

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeRequest(user?: { sub?: string; role?: string; status?: string }) {
  return { user } as { user?: { sub?: string; role?: string; status?: string } };
}

// ---------------------------------------------------------------------------
// Test suite
// ---------------------------------------------------------------------------

describe('ApiBlogPostController', () => {
  let controller: ApiBlogPostController;

  beforeEach(() => {
    vi.clearAllMocks();
    controller = new ApiBlogPostController(apiBlogPostServiceMock as never);
  });

  it('should be defined', () => {
    expect(controller).toBeDefined();
  });

  // ==========================================================================
  // getActorIdFromRequest (tested via public methods)
  // ==========================================================================

  describe('getActorIdFromRequest (via public methods)', () => {
    describe('create', () => {
      it('extracts actorId from request and passes to service', async () => {
        const dto = { title: 'Test' } as never;
        const req = makeRequest({ sub: 'user-abc' });
        apiBlogPostServiceMock.create.mockResolvedValue({ id: 'post-1' });

        await controller.create(dto, req);

        expect(apiBlogPostServiceMock.create).toHaveBeenCalledWith(dto, 'user-abc');
      });

      it('throws UnauthorizedException when req.user is undefined', async () => {
        const req = {} as never;

        await expect(controller.create({} as never, req)).rejects.toThrow(UnauthorizedException);
      });

      it('throws UnauthorizedException when req.user.sub is undefined', async () => {
        const req = makeRequest({ sub: undefined as unknown as string });

        await expect(controller.create({} as never, req)).rejects.toThrow(UnauthorizedException);
      });

      it('throws UnauthorizedException when req.user.sub is missing entirely', async () => {
        const req = makeRequest({} as { sub?: string });

        await expect(controller.create({} as never, req)).rejects.toThrow(UnauthorizedException);
      });

      it('returns the service result', async () => {
        const dto = { title: 'My Post' } as never;
        const req = makeRequest({ sub: 'user-1' });
        const serviceResult = { id: 'post-id-42' };
        apiBlogPostServiceMock.create.mockResolvedValue(serviceResult);

        const result = await controller.create(dto, req);

        expect(result).toBe(serviceResult);
      });
    });

    describe('update', () => {
      it('extracts actorId and passes id + DTO + actorId to service', async () => {
        const req = makeRequest({ sub: 'user-xyz' });
        const dto = { title: 'Updated Title' } as never;
        apiBlogPostServiceMock.update.mockResolvedValue({ title: 'Updated Title' });

        await controller.update('post-id-1', dto, req);

        expect(apiBlogPostServiceMock.update).toHaveBeenCalledWith('post-id-1', dto, 'user-xyz');
      });

      it('throws UnauthorizedException when req.user is undefined', async () => {
        const req = {} as never;

        await expect(controller.update('post-id-1', {} as never, req)).rejects.toThrow(UnauthorizedException);
      });

      it('throws UnauthorizedException when req.user.sub is undefined', async () => {
        const req = makeRequest({ sub: undefined as unknown as string });

        await expect(controller.update('post-id-1', {} as never, req)).rejects.toThrow(UnauthorizedException);
      });

      it('returns the service result', async () => {
        const req = makeRequest({ sub: 'user-2' });
        const serviceResult = { title: 'Updated' };
        apiBlogPostServiceMock.update.mockResolvedValue(serviceResult);

        const result = await controller.update('post-id-2', { title: 'Updated' } as never, req);

        expect(result).toBe(serviceResult);
      });
    });

    describe('delete', () => {
      it('extracts actorId and passes id + actorId to service', async () => {
        const req = makeRequest({ sub: 'user-deleter' });
        apiBlogPostServiceMock.delete.mockResolvedValue({ title: 'Deleted' });

        await controller.delete('post-id-3', req);

        expect(apiBlogPostServiceMock.delete).toHaveBeenCalledWith('post-id-3', 'user-deleter');
      });

      it('throws UnauthorizedException when req.user is undefined', async () => {
        const req = {} as never;

        await expect(controller.delete('post-id-3', req)).rejects.toThrow(UnauthorizedException);
      });

      it('throws UnauthorizedException when req.user.sub is undefined', async () => {
        const req = makeRequest({ sub: undefined as unknown as string });

        await expect(controller.delete('post-id-3', req)).rejects.toThrow(UnauthorizedException);
      });

      it('returns the service result', async () => {
        const req = makeRequest({ sub: 'user-3' });
        const serviceResult = { title: 'Deleted Post' };
        apiBlogPostServiceMock.delete.mockResolvedValue(serviceResult);

        const result = await controller.delete('post-id-4', req);

        expect(result).toBe(serviceResult);
      });
    });
  });

  // ==========================================================================
  // findAll
  // ==========================================================================

  describe('findAll', () => {
    it('passes pagination params (pagesize, page) to service with defaults', async () => {
      const pagination = { page: 0, pagesize: 10 };
      apiBlogPostServiceMock.findAll.mockResolvedValue({ posts: [], count: 0 });

      await controller.findAll(pagination);

      expect(apiBlogPostServiceMock.findAll).toHaveBeenCalledWith(10, 0);
    });

    it('passes custom pagination values to service', async () => {
      const pagination = { page: 3, pagesize: 25 };
      apiBlogPostServiceMock.findAll.mockResolvedValue({ posts: [], count: 0 });

      await controller.findAll(pagination);

      expect(apiBlogPostServiceMock.findAll).toHaveBeenCalledWith(25, 3);
    });

    it('returns the service result', async () => {
      const serviceResult = { posts: [{ title: 'P1' } as never], count: 1 };
      apiBlogPostServiceMock.findAll.mockResolvedValue(serviceResult);

      const result = await controller.findAll({ page: 0, pagesize: 10 });

      expect(result).toBe(serviceResult);
    });
  });

  // ==========================================================================
  // findOne
  // ==========================================================================

  describe('findOne', () => {
    it('passes id to service', async () => {
      apiBlogPostServiceMock.findOne.mockResolvedValue({ title: 'Found' });

      await controller.findOne('post-id-99');

      expect(apiBlogPostServiceMock.findOne).toHaveBeenCalledWith('post-id-99');
    });

    it('returns the service result', async () => {
      const serviceResult = { title: 'A Post' };
      apiBlogPostServiceMock.findOne.mockResolvedValue(serviceResult);

      const result = await controller.findOne('post-id-100');

      expect(result).toBe(serviceResult);
    });

    it('returns undefined when service returns undefined', async () => {
      apiBlogPostServiceMock.findOne.mockResolvedValue(undefined);

      const result = await controller.findOne('nonexistent');

      expect(result).toBeUndefined();
    });
  });
});
