import { BadRequestException, NotFoundException } from '@nestjs/common';
import { Types } from 'mongoose';
import { vi } from 'vitest';

// ---------------------------------------------------------------------------
// Stub the schema module so @Prop / @Schema decorators never evaluate.
// ---------------------------------------------------------------------------
vi.mock('./schemas/project.schema', () => {
  class Project {}
  return {
    Project,
    ProjectDocument: {} as never,
    ProjectSchema: {},
  };
});

import { ProjectService } from './project.service';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Build a minimal mock project object returned by model operations. */
function mockProject(overrides: Record<string, unknown> = {}) {
  return {
    _id: new Types.ObjectId(),
    title: 'Test Project',
    slug: 'test-project',
    description: 'A test project',
    thumbnail: '/media/images/test.webp',
    dateTimeCreated: '2024-01-01T00:00:00.000Z',
    creator: new Types.ObjectId(),
    batchDownloadLinks: [],
    updatedBy: undefined,
    updatedAt: undefined,
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// describe('ProjectService')
// ---------------------------------------------------------------------------

describe('ProjectService', () => {
  let service: ProjectService;

  // -- mock collaborators ---------------------------------------------------

  const mockFindOneLean = vi.fn();

  /** The chain returned by findOne().select('_id').lean() inside createUniqueSlug. */
  const slugFindOneChain = {
    select: vi.fn().mockReturnValue({ lean: mockFindOneLean }),
  };

  const findQueryExec = vi.fn();
  const findQueryChain = {
    populate: vi.fn().mockReturnThis(),
    sort: vi.fn().mockReturnThis(),
    skip: vi.fn().mockReturnThis(),
    limit: vi.fn().mockReturnThis(),
    exec: findQueryExec,
  };

  const findByIdExec = vi.fn();
  const findByIdChain = {
    select: vi.fn().mockReturnValue({ exec: findByIdExec }),
  };

  const findByIdAndUpdateExec = vi.fn();
  const findByIdAndUpdateChain = {
    exec: findByIdAndUpdateExec,
  };

  const findByIdAndDeleteExec = vi.fn();
  const findByIdAndDeleteChain = {
    exec: findByIdAndDeleteExec,
  };

  const projectModelMock = {
    create: vi.fn(),
    find: vi.fn().mockReturnValue(findQueryChain),
    findOne: vi.fn().mockReturnValue(slugFindOneChain), // default: createUniqueSlug path
    findById: vi.fn().mockReturnValue(findByIdChain),
    findByIdAndUpdate: vi.fn().mockReturnValue(findByIdAndUpdateChain),
    findByIdAndDelete: vi.fn().mockReturnValue(findByIdAndDeleteChain),
    countDocuments: vi.fn(),
  };

  const mediaServiceMock = {
    deleteManagedImageByUrl: vi.fn(),
  };

  // -- lifecycle ------------------------------------------------------------

  beforeEach(() => {
    vi.clearAllMocks();

    // Reset default findOne chain for createUniqueSlug
    projectModelMock.findOne = vi.fn().mockReturnValue(slugFindOneChain);

    service = new ProjectService(projectModelMock as never, mediaServiceMock as never);
  });

  // =========================================================================
  // create()
  // =========================================================================

  describe('create', () => {
    const creatorId = new Types.ObjectId().toHexString();
    const dto = {
      title: 'My Test Project',
      description: 'Some description',
      thumbnail: '/media/images/thumb.webp',
      dateTimeCreated: '2024-01-01T00:00:00.000Z',
    };

    it('creates a project with an auto-generated slug', async () => {
      mockFindOneLean.mockResolvedValue(null); // no slug collision
      const created = mockProject({ title: 'My Test Project', slug: 'my-test-project' });
      projectModelMock.create.mockResolvedValue(created);

      const result = await service.create(dto, creatorId);

      expect(result.slug).toBe('my-test-project');
      expect(projectModelMock.create).toHaveBeenCalledWith(
        expect.objectContaining({ slug: 'my-test-project', creator: expect.any(Types.ObjectId) }),
      );
    });

    it('appends a counter suffix when base slug already exists', async () => {
      // first lookup finds an existing document, second succeeds
      mockFindOneLean.mockResolvedValueOnce({ _id: 'existing' }).mockResolvedValueOnce(null);

      projectModelMock.create.mockResolvedValue(mockProject({ slug: 'my-test-project-2' }));

      const result = await service.create(dto, creatorId);

      expect(result.slug).toBe('my-test-project-2');
      expect(mockFindOneLean).toHaveBeenCalledTimes(2);
    });

    it('falls back to "project" slug when title yields empty string', async () => {
      mockFindOneLean.mockResolvedValue(null);
      projectModelMock.create.mockResolvedValue(mockProject({ slug: 'project' }));

      // A title consisting only of Greek letters will be stripped to empty
      const result = await service.create({ ...dto, title: 'Καλημέρα' }, creatorId);

      expect(result.slug).toBe('project');
    });

    it('sets the creator ref to the provided creatorId', async () => {
      mockFindOneLean.mockResolvedValue(null);
      projectModelMock.create.mockResolvedValue(mockProject());

      await service.create(dto, creatorId);

      expect(projectModelMock.create).toHaveBeenCalledWith(expect.objectContaining({ creator: expect.any(Types.ObjectId) }));
      const callArg = projectModelMock.create.mock.calls[0][0];
      expect(callArg.creator.toHexString()).toBe(creatorId);
    });

    // -- validateBatchLinks -------------------------------------------------

    it('accepts undefined batchDownloadLinks', async () => {
      mockFindOneLean.mockResolvedValue(null);
      projectModelMock.create.mockResolvedValue(mockProject());

      await expect(service.create({ ...dto, batchDownloadLinks: undefined } as never, creatorId)).resolves.toBeDefined();
    });

    it('accepts an empty batchDownloadLinks array', async () => {
      mockFindOneLean.mockResolvedValue(null);
      projectModelMock.create.mockResolvedValue(mockProject());

      await expect(service.create({ ...dto, batchDownloadLinks: [] }, creatorId)).resolves.toBeDefined();
    });

    it('accepts a link with a complete 1080p torrent+magnet pair', async () => {
      mockFindOneLean.mockResolvedValue(null);
      projectModelMock.create.mockResolvedValue(mockProject());

      await expect(
        service.create(
          {
            ...dto,
            batchDownloadLinks: [
              {
                name: 'Ep 1',
                downloadLinkTorrent: 'https://example.com/torrent',
                downloadLink: 'magnet:?xt=urn:btih:abc',
              },
            ],
          },
          creatorId,
        ),
      ).resolves.toBeDefined();
    });

    it('accepts a link with a complete 2160p torrent+magnet pair', async () => {
      mockFindOneLean.mockResolvedValue(null);
      projectModelMock.create.mockResolvedValue(mockProject());

      await expect(
        service.create(
          {
            ...dto,
            batchDownloadLinks: [
              {
                name: 'Ep 1',
                downloadLink4kTorrent: 'https://example.com/4k.torrent',
                downloadLink4k: 'magnet:?xt=urn:btih:def',
              },
            ],
          },
          creatorId,
        ),
      ).resolves.toBeDefined();
    });

    it('rejects a batch link missing the torrent (1080p incomplete pair)', async () => {
      await expect(
        service.create(
          {
            ...dto,
            batchDownloadLinks: [{ name: 'Ep 1', downloadLink: 'magnet:?xt=urn:btih:abc' }],
          },
          creatorId,
        ),
      ).rejects.toThrow(BadRequestException);
    });

    it('rejects a batch link missing the magnet (1080p incomplete pair)', async () => {
      await expect(
        service.create(
          {
            ...dto,
            batchDownloadLinks: [{ name: 'Ep 1', downloadLinkTorrent: 'https://example.com/torrent' }],
          },
          creatorId,
        ),
      ).rejects.toThrow(BadRequestException);
    });

    it('rejects a batch link with only names and no resolution pair', async () => {
      await expect(
        service.create(
          {
            ...dto,
            batchDownloadLinks: [{ name: 'Ep 1' }],
          },
          creatorId,
        ),
      ).rejects.toThrow(BadRequestException);
    });

    it('rejects a batch link where downloadLink is empty string', async () => {
      await expect(
        service.create(
          {
            ...dto,
            batchDownloadLinks: [{ name: 'Ep 1', downloadLinkTorrent: 'https://example.com/torrent', downloadLink: '   ' }],
          },
          creatorId,
        ),
      ).rejects.toThrow(BadRequestException);
    });
  });

  // =========================================================================
  // slugify() — tested indirectly via create() and update()
  // =========================================================================

  describe('slugify (via create)', () => {
    const creatorId = new Types.ObjectId().toHexString();

    beforeEach(() => {
      mockFindOneLean.mockResolvedValue(null);
      projectModelMock.create.mockImplementation((data: Record<string, unknown>) =>
        Promise.resolve(mockProject({ slug: data.slug })),
      );
    });

    it('converts "Hello World!" to "hello-world"', async () => {
      const result = await service.create(
        {
          title: 'Hello World!',
          description: 'desc',
          thumbnail: '/t.webp',
          dateTimeCreated: '2024-01-01T00:00:00.000Z',
        },
        creatorId,
      );
      expect(result.slug).toBe('hello-world');
    });

    it('converts Greek characters and special punctuation', async () => {
      // Greek letters are stripped; only ASCII letters, digits, spaces, and hyphens survive
      const result = await service.create(
        {
          title: 'Greek: Καλημέρα!',
          description: 'desc',
          thumbnail: '/t.webp',
          dateTimeCreated: '2024-01-01T00:00:00.000Z',
        },
        creatorId,
      );
      expect(result.slug).toBe('greek-');
    });

    it('handles multiple spaces and hyphens', async () => {
      const result = await service.create(
        {
          title: '  Many   Spaces --- Here  ',
          description: 'desc',
          thumbnail: '/t.webp',
          dateTimeCreated: '2024-01-01T00:00:00.000Z',
        },
        creatorId,
      );
      expect(result.slug).toBe('many-spaces-here');
    });

    it('strips non-ASCII characters completely', async () => {
      const result = await service.create(
        {
          title: 'Café & Résumé — Special',
          description: 'desc',
          thumbnail: '/t.webp',
          dateTimeCreated: '2024-01-01T00:00:00.000Z',
        },
        creatorId,
      );
      // é → e after NFKD + accent removal; & and — are stripped
      expect(result.slug).toBe('cafe-resume-special');
    });

    it('increments counter when slug collides (uniqueness)', async () => {
      mockFindOneLean.mockReset();
      mockFindOneLean
        .mockResolvedValueOnce({ _id: 'existing-1' }) // my-test → taken
        .mockResolvedValueOnce({ _id: 'existing-2' }) // my-test-2 → taken
        .mockResolvedValueOnce(null); // my-test-3 → free

      const result = await service.create(
        {
          title: 'My Test',
          description: 'desc',
          thumbnail: '/t.webp',
          dateTimeCreated: '2024-01-01T00:00:00.000Z',
        },
        creatorId,
      );
      expect(result.slug).toBe('my-test-3');
    });
  });

  // =========================================================================
  // findAll()
  // =========================================================================

  describe('findAll', () => {
    it('returns paginated projects with count', async () => {
      const projects = [mockProject({ slug: 'p1' }), mockProject({ slug: 'p2' })];
      findQueryExec.mockResolvedValue(projects);
      projectModelMock.countDocuments.mockResolvedValue(42);

      const result = await service.findAll(10, 0);

      expect(result).toEqual({ projects, count: 42 });
    });

    it('applies skip with pageSize * currentPage', async () => {
      findQueryExec.mockResolvedValue([]);
      projectModelMock.countDocuments.mockResolvedValue(0);

      await service.findAll(20, 3);

      expect(findQueryChain.skip).toHaveBeenCalledWith(60);
      expect(findQueryChain.limit).toHaveBeenCalledWith(20);
    });

    it('populates creator and updatedBy with username and avatar', async () => {
      findQueryExec.mockResolvedValue([]);
      projectModelMock.countDocuments.mockResolvedValue(0);

      await service.findAll(10, 0);

      expect(findQueryChain.populate).toHaveBeenCalledWith('creator', 'username avatar');
    });

    it('sanitizes projects by clearing updatedAt when updatedBy is absent', async () => {
      const withUpdatedBy = mockProject({
        slug: 'with',
        updatedBy: new Types.ObjectId(),
        updatedAt: new Date('2024-06-01'),
      });
      const withoutUpdatedBy = mockProject({
        slug: 'without',
        updatedBy: undefined,
        updatedAt: new Date('2024-05-01'),
      });
      findQueryExec.mockResolvedValue([withUpdatedBy, withoutUpdatedBy]);
      projectModelMock.countDocuments.mockResolvedValue(2);

      const result = await service.findAll(10, 0);

      // updatedAt should remain when updatedBy is set
      expect(result.projects[0].updatedAt).toBeDefined();
      // updatedAt should be cleared when updatedBy is absent
      expect(result.projects[1].updatedAt).toBeUndefined();
    });
  });

  // =========================================================================
  // findOne()
  // =========================================================================

  describe('findOne', () => {
    it('returns a populated project when found', async () => {
      const project = mockProject();
      // Override findOne to use populate chain
      projectModelMock.findOne = vi.fn().mockReturnValue({
        populate: vi.fn().mockReturnValue({
          populate: vi.fn().mockResolvedValue(project),
        }),
      });

      const result = await service.findOne(project._id.toHexString());

      expect(result).toEqual(project);
      expect(projectModelMock.findOne).toHaveBeenCalledWith({ _id: project._id.toHexString() });
    });

    it('throws NotFoundException when project is not found', async () => {
      projectModelMock.findOne = vi.fn().mockReturnValue({
        populate: vi.fn().mockReturnValue({
          populate: vi.fn().mockResolvedValue(null),
        }),
      });

      await expect(service.findOne(new Types.ObjectId().toHexString())).rejects.toThrow(NotFoundException);
    });

    it('sanitizes edited metadata on the returned project', async () => {
      const project = mockProject({ updatedBy: undefined, updatedAt: new Date('2024-01-01') });
      projectModelMock.findOne = vi.fn().mockReturnValue({
        populate: vi.fn().mockReturnValue({
          populate: vi.fn().mockResolvedValue(project),
        }),
      });

      const result = await service.findOne(project._id.toHexString());

      expect(result!.updatedAt).toBeUndefined();
    });
  });

  // =========================================================================
  // update()
  // =========================================================================

  describe('update', () => {
    const actorId = new Types.ObjectId().toHexString();
    const projectId = new Types.ObjectId().toHexString();

    beforeEach(() => {
      // Reset findOne to slug chain default (used by createUniqueSlug)
      projectModelMock.findOne = vi.fn().mockReturnValue(slugFindOneChain);
    });

    it('throws NotFoundException when project does not exist', async () => {
      findByIdExec.mockResolvedValue(null);

      await expect(service.update(projectId, { title: 'New' }, actorId)).rejects.toThrow(NotFoundException);
    });

    it('updates the project and sets updatedBy', async () => {
      const existing = mockProject({ _id: projectId, thumbnail: '/media/images/old.webp' });
      findByIdExec.mockResolvedValue(existing);
      mockFindOneLean.mockResolvedValue(null); // no slug collision
      const updated = mockProject({
        _id: projectId,
        title: 'New Title',
        slug: 'new-title',
        thumbnail: '/media/images/old.webp',
      });
      findByIdAndUpdateExec.mockResolvedValue(updated);

      const result = await service.update(projectId, { title: 'New Title' }, actorId);

      expect(findByIdAndUpdateExec).toHaveBeenCalled();
      const updateCall = projectModelMock.findByIdAndUpdate.mock.calls[0];
      expect(updateCall[0]).toBe(projectId);
      expect(updateCall[1]).toMatchObject({
        title: 'New Title',
        slug: 'new-title',
      });
      expect((updateCall[1] as Record<string, unknown>).updatedBy.toHexString()).toBe(actorId);
      expect(result).toEqual(updated);
    });

    it('regenerates slug when title is changed', async () => {
      const existing = mockProject({ _id: projectId, thumbnail: '/old.webp' });
      findByIdExec.mockResolvedValue(existing);
      mockFindOneLean.mockResolvedValue(null);
      findByIdAndUpdateExec.mockResolvedValue(mockProject({ slug: 'updated-title' }));

      await service.update(projectId, { title: 'Updated Title' }, actorId);

      const updatePayload = projectModelMock.findByIdAndUpdate.mock.calls[0][1] as Record<string, unknown>;
      expect(updatePayload.slug).toBe('updated-title');
    });

    it('does not regenerate slug when title is unchanged', async () => {
      const existing = mockProject({ _id: projectId, thumbnail: '/old.webp' });
      findByIdExec.mockResolvedValue(existing);
      // createUniqueSlug should NOT be called
      findByIdAndUpdateExec.mockResolvedValue(mockProject());

      await service.update(projectId, { description: 'New description' }, actorId);

      const updatePayload = projectModelMock.findByIdAndUpdate.mock.calls[0][1] as Record<string, unknown>;
      expect(updatePayload.slug).toBeUndefined();
    });

    it('validates batch links when provided in update', async () => {
      const existing = mockProject({ _id: projectId, thumbnail: '/old.webp' });
      findByIdExec.mockResolvedValue(existing);

      await expect(
        service.update(
          projectId,
          {
            batchDownloadLinks: [{ name: 'Ep 1' }], // incomplete
          },
          actorId,
        ),
      ).rejects.toThrow(BadRequestException);
    });

    it('accepts valid batch links in update', async () => {
      const existing = mockProject({ _id: projectId, thumbnail: '/old.webp' });
      findByIdExec.mockResolvedValue(existing);
      findByIdAndUpdateExec.mockResolvedValue(mockProject());

      await expect(
        service.update(
          projectId,
          {
            batchDownloadLinks: [
              {
                name: 'Ep 1',
                downloadLinkTorrent: 'https://example.com/torrent',
                downloadLink: 'magnet:?xt=urn:btih:abc',
              },
            ],
          },
          actorId,
        ),
      ).resolves.toBeDefined();
    });

    it('cleans up old thumbnail when thumbnail changes', async () => {
      const existing = mockProject({ _id: projectId, thumbnail: '/media/images/old.webp' });
      findByIdExec.mockResolvedValue(existing);
      findByIdAndUpdateExec.mockResolvedValue(mockProject({ _id: projectId, thumbnail: '/media/images/new.webp' }));

      await service.update(projectId, { thumbnail: '/media/images/new.webp' }, actorId);

      expect(mediaServiceMock.deleteManagedImageByUrl).toHaveBeenCalledWith('/media/images/old.webp');
    });

    it('does not clean up old thumbnail when thumbnail is unchanged', async () => {
      const thumbnail = '/media/images/same.webp';
      const existing = mockProject({ _id: projectId, thumbnail });
      findByIdExec.mockResolvedValue(existing);
      findByIdAndUpdateExec.mockResolvedValue(mockProject({ _id: projectId, thumbnail }));

      await service.update(projectId, { title: 'Only title change' }, actorId);

      expect(mediaServiceMock.deleteManagedImageByUrl).not.toHaveBeenCalled();
    });

    it('handles slug collision during update with excludedId', async () => {
      const existing = mockProject({ _id: projectId, thumbnail: '/old.webp' });
      findByIdExec.mockResolvedValue(existing);
      mockFindOneLean
        .mockResolvedValueOnce({ _id: 'other' }) // new-title taken
        .mockResolvedValueOnce(null); // new-title-2 free
      findByIdAndUpdateExec.mockResolvedValue(mockProject({ slug: 'new-title-2' }));

      const result = await service.update(projectId, { title: 'New Title' }, actorId);

      expect(result!.slug).toBe('new-title-2');
      // First findOne call should include $ne: projectId
      expect(projectModelMock.findOne).toHaveBeenCalledWith(
        expect.objectContaining({ slug: 'new-title', _id: { $ne: projectId } }),
      );
    });
  });

  // =========================================================================
  // remove()
  // =========================================================================

  describe('remove', () => {
    it('deletes the project and cleans up its thumbnail', async () => {
      const project = mockProject({ thumbnail: '/media/images/to-delete.webp' });
      findByIdAndDeleteExec.mockResolvedValue(project);

      const result = await service.remove(project._id.toHexString());

      expect(result).toEqual(project);
      expect(projectModelMock.findByIdAndDelete).toHaveBeenCalledWith(project._id.toHexString());
      expect(mediaServiceMock.deleteManagedImageByUrl).toHaveBeenCalledWith('/media/images/to-delete.webp');
    });

    it('throws NotFoundException when project does not exist', async () => {
      findByIdAndDeleteExec.mockResolvedValue(null);

      await expect(service.remove(new Types.ObjectId().toHexString())).rejects.toThrow(NotFoundException);
      expect(mediaServiceMock.deleteManagedImageByUrl).not.toHaveBeenCalled();
    });
  });
});
