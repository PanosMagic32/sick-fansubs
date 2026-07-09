import { UnauthorizedException } from '@nestjs/common';
import { Types } from 'mongoose';
import { vi } from 'vitest';

// ---------------------------------------------------------------------------
// Stub @api/shared so PaginationDto / ParseMongoIdPipe decorators never evaluate.
// ---------------------------------------------------------------------------
vi.mock('@api/shared', () => ({
  PaginationDto: class PaginationDto {},
  ParseMongoIdPipe: class ParseMongoIdPipe {},
}));

import { ProjectController } from './project.controller';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function mockReq(user?: { sub?: string }) {
  return { user } as { user?: { sub?: string } };
}

// ---------------------------------------------------------------------------
// describe('ProjectController')
// ---------------------------------------------------------------------------

describe('ProjectController', () => {
  let controller: ProjectController;

  const projectServiceMock = {
    create: vi.fn(),
    findAll: vi.fn(),
    findOne: vi.fn(),
    update: vi.fn(),
    remove: vi.fn(),
  };

  beforeEach(() => {
    vi.clearAllMocks();
    controller = new ProjectController(projectServiceMock as never);
  });

  it('should be defined', () => {
    expect(controller).toBeDefined();
  });

  // =========================================================================
  // create()
  // =========================================================================

  describe('create', () => {
    const dto = {
      title: 'New Project',
      description: 'Description',
      thumbnail: '/media/images/thumb.webp',
      dateTimeCreated: '2024-01-01T00:00:00.000Z',
    };

    it('extracts actorId from request and passes DTO + actorId to service', async () => {
      const actorId = new Types.ObjectId().toHexString();
      const created = { _id: new Types.ObjectId(), slug: 'new-project', ...dto };
      projectServiceMock.create.mockResolvedValue(created);

      const result = await controller.create(dto, mockReq({ sub: actorId }));

      expect(result).toEqual(created);
      expect(projectServiceMock.create).toHaveBeenCalledWith(dto, actorId);
    });

    it('throws UnauthorizedException when req.user is undefined', async () => {
      await expect(controller.create(dto, {} as never)).rejects.toThrow(UnauthorizedException);
    });

    it('throws UnauthorizedException when req.user.sub is undefined', async () => {
      await expect(controller.create(dto, mockReq({}))).rejects.toThrow(UnauthorizedException);
    });
  });

  // =========================================================================
  // findAll()
  // =========================================================================

  describe('findAll', () => {
    it('passes pagination params to service', async () => {
      const result = { projects: [], count: 0 };
      projectServiceMock.findAll.mockResolvedValue(result);

      const pagination = { pagesize: 10, page: 2 };
      const response = await controller.findAll(pagination as never);

      expect(response).toEqual(result);
      expect(projectServiceMock.findAll).toHaveBeenCalledWith(10, 2);
    });
  });

  // =========================================================================
  // findOne()
  // =========================================================================

  describe('findOne', () => {
    it('passes id to service', async () => {
      const id = new Types.ObjectId().toHexString();
      const project = { _id: id, slug: 'test' };
      projectServiceMock.findOne.mockResolvedValue(project);

      const result = await controller.findOne(id);

      expect(result).toEqual(project);
      expect(projectServiceMock.findOne).toHaveBeenCalledWith(id);
    });
  });

  // =========================================================================
  // update()
  // =========================================================================

  describe('update', () => {
    const id = new Types.ObjectId().toHexString();
    const dto = { title: 'Updated Title' };

    it('extracts actorId from request and passes id + DTO + actorId to service', async () => {
      const actorId = new Types.ObjectId().toHexString();
      const updated = { _id: id, slug: 'updated-title', ...dto };
      projectServiceMock.update.mockResolvedValue(updated);

      const result = await controller.update(id, dto, mockReq({ sub: actorId }));

      expect(result).toEqual(updated);
      expect(projectServiceMock.update).toHaveBeenCalledWith(id, dto, actorId);
    });

    it('throws UnauthorizedException when req.user is undefined', async () => {
      await expect(controller.update(id, dto, {} as never)).rejects.toThrow(UnauthorizedException);
    });

    it('throws UnauthorizedException when req.user.sub is undefined', async () => {
      await expect(controller.update(id, dto, mockReq({}))).rejects.toThrow(UnauthorizedException);
    });
  });

  // =========================================================================
  // remove()
  // =========================================================================

  describe('remove', () => {
    it('passes id to service', async () => {
      const id = new Types.ObjectId().toHexString();
      const deleted = { _id: id, slug: 'deleted' };
      projectServiceMock.remove.mockResolvedValue(deleted);

      const result = await controller.remove(id);

      expect(result).toEqual(deleted);
      expect(projectServiceMock.remove).toHaveBeenCalledWith(id);
    });
  });

  // =========================================================================
  // getActorIdFromRequest (tested via public methods)
  // =========================================================================

  describe('getActorIdFromRequest (via public methods)', () => {
    it('throws UnauthorizedException when no user.sub in create', async () => {
      await expect(
        controller.create(
          {
            title: 'X',
            description: 'X',
            thumbnail: '/x.webp',
            dateTimeCreated: '2024-01-01T00:00:00.000Z',
          },
          mockReq(),
        ),
      ).rejects.toThrow(UnauthorizedException);
    });

    it('throws UnauthorizedException when no user.sub in update', async () => {
      await expect(controller.update(new Types.ObjectId().toHexString(), {}, mockReq())).rejects.toThrow(
        UnauthorizedException,
      );
    });

    it('throws UnauthorizedException when user sub is explicitly missing in create', async () => {
      await expect(
        controller.create(
          {
            title: 'X',
            description: 'X',
            thumbnail: '/x.webp',
            dateTimeCreated: '2024-01-01T00:00:00.000Z',
          },
          mockReq({ sub: undefined }),
        ),
      ).rejects.toThrow(UnauthorizedException);
    });

    it('throws UnauthorizedException when user sub is explicitly missing in update', async () => {
      await expect(controller.update(new Types.ObjectId().toHexString(), {}, mockReq({ sub: undefined }))).rejects.toThrow(
        UnauthorizedException,
      );
    });
  });
});
