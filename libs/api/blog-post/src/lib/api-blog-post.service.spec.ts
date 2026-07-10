import { vi } from 'vitest';

// ---------------------------------------------------------------------------
// Prevent module-level decorator errors by mocking modules that use
// class-validator / @nestjs/mongoose @Prop without explicit `type`.
// ---------------------------------------------------------------------------
vi.mock('./schemas/blog-post.schema', () => ({
  BlogPost: class BlogPost {},
  BlogPostDocument: class BlogPostDocument {},
}));

vi.mock('@api/media', () => ({
  MediaService: class MediaService {},
}));

// ---------------------------------------------------------------------------
import { BadRequestException, NotFoundException } from '@nestjs/common';
import { Types } from 'mongoose';

import { ApiBlogPostService } from './api-blog-post.service';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Creates a mongoose‑like thenable query mock so both `await` and `.exec()` work. */
function makeThenableQuery<T>(result: T) {
  const exec = vi.fn().mockResolvedValue(result);
  const query: Record<string, unknown> = {
    populate: vi.fn().mockReturnThis(),
    sort: vi.fn().mockReturnThis(),
    skip: vi.fn().mockReturnThis(),
    limit: vi.fn().mockReturnThis(),
    select: vi.fn().mockReturnThis(),
    exec,
    then(onFulfilled: (v: T) => unknown) {
      return exec().then(onFulfilled);
    },
  };
  return query;
}

/** Minimal blog‑post document shape returned by model methods. */
function stubPost(overrides: Record<string, unknown> = {}) {
  return {
    _id: new Types.ObjectId('aaaaaaaaaaaaaaaaaaaaaaaa'),
    title: 'Default Title',
    subtitle: 'Default Subtitle',
    description: 'Default Description',
    thumbnail: 'https://example.com/thumb.jpg',
    downloadLink: undefined,
    downloadLinkTorrent: undefined,
    downloadLink4k: undefined,
    downloadLink4kTorrent: undefined,
    dateTimeCreated: '2025-01-01T00:00:00.000Z',
    creator: new Types.ObjectId('bbbbbbbbbbbbbbbbbbbbbbbb'),
    updatedBy: undefined,
    updatedAt: undefined,
    toObject() {
      return { ...this };
    },
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const blogPostModelMock = {
  create: vi.fn(),
  find: vi.fn(),
  findOne: vi.fn(),
  findById: vi.fn(),
  findByIdAndUpdate: vi.fn(),
  findByIdAndDelete: vi.fn(),
  countDocuments: vi.fn(),
};

const mediaServiceMock = {
  deleteManagedImageByUrl: vi.fn(),
};

const userServiceMock = {
  addCreatedBlogPost: vi.fn(),
  removeCreatedBlogPost: vi.fn(),
};

// ---------------------------------------------------------------------------
// Test suite
// ---------------------------------------------------------------------------

describe('ApiBlogPostService', () => {
  let service: ApiBlogPostService;

  beforeEach(() => {
    vi.clearAllMocks();

    service = new ApiBlogPostService(blogPostModelMock as never, mediaServiceMock as never, userServiceMock as never);
  });

  // -- Basic existence -------------------------------------------------------

  it('should be defined', () => {
    expect(service).toBeDefined();
  });

  // ==========================================================================
  // validateResolution (tested through create / update)
  // ==========================================================================

  describe('validateResolution (via create)', () => {
    const validDto = {
      title: 'T',
      subtitle: 'S',
      description: 'D',
      thumbnail: 'https://example.com/t.jpg',
      dateTimeCreated: '2025-01-01T00:00:00.000Z',
    };

    const mockCreatedDoc = { _id: new Types.ObjectId('cccccccccccccccccccccccc'), toObject: () => ({}) };

    beforeEach(() => {
      blogPostModelMock.create.mockResolvedValue(mockCreatedDoc);
      userServiceMock.addCreatedBlogPost.mockResolvedValue(undefined);
    });

    it('throws BadRequestException when no resolution links are provided', async () => {
      await expect(service.create(validDto, 'creator-1')).rejects.toThrow(BadRequestException);
    });

    it('throws BadRequestException when only downloadLink (magnet) is provided without torrent', async () => {
      await expect(service.create({ ...validDto, downloadLink: 'magnet:?xt=urn:btih:abc' }, 'creator-1')).rejects.toThrow(
        BadRequestException,
      );
    });

    it('throws BadRequestException when only downloadLinkTorrent is provided without magnet', async () => {
      await expect(
        service.create({ ...validDto, downloadLinkTorrent: 'https://example.com/file.torrent' }, 'creator-1'),
      ).rejects.toThrow(BadRequestException);
    });

    it('throws BadRequestException when only downloadLink4k is provided without 4k torrent', async () => {
      await expect(
        service.create({ ...validDto, downloadLink4k: 'magnet:?xt=urn:btih:abc4k' }, 'creator-1'),
      ).rejects.toThrow(BadRequestException);
    });

    it('throws BadRequestException when only downloadLink4kTorrent is provided without 4k magnet', async () => {
      await expect(
        service.create({ ...validDto, downloadLink4kTorrent: 'https://example.com/file4k.torrent' }, 'creator-1'),
      ).rejects.toThrow(BadRequestException);
    });

    it('throws BadRequestException when links are whitespace-only strings', async () => {
      await expect(
        service.create(
          {
            ...validDto,
            downloadLink: '   ',
            downloadLinkTorrent: '\t',
          },
          'creator-1',
        ),
      ).rejects.toThrow(BadRequestException);
    });

    it('succeeds with a complete 1080p resolution pair', async () => {
      await service.create(
        {
          ...validDto,
          downloadLink: 'magnet:?xt=urn:btih:abc',
          downloadLinkTorrent: 'https://example.com/file.torrent',
        },
        'aaaaaaaaaaaaaaaaaaaaaaa1',
      );

      expect(blogPostModelMock.create).toHaveBeenCalledTimes(1);
    });

    it('succeeds with a complete 2160p (4k) resolution pair', async () => {
      await service.create(
        {
          ...validDto,
          downloadLink4k: 'magnet:?xt=urn:btih:abc4k',
          downloadLink4kTorrent: 'https://example.com/file4k.torrent',
        },
        'aaaaaaaaaaaaaaaaaaaaaaa1',
      );

      expect(blogPostModelMock.create).toHaveBeenCalledTimes(1);
    });

    it('succeeds when both 1080p and 2160p pairs are provided', async () => {
      await service.create(
        {
          ...validDto,
          downloadLink: 'magnet:?xt=urn:btih:abc',
          downloadLinkTorrent: 'https://example.com/file.torrent',
          downloadLink4k: 'magnet:?xt=urn:btih:abc4k',
          downloadLink4kTorrent: 'https://example.com/file4k.torrent',
        },
        'aaaaaaaaaaaaaaaaaaaaaaa1',
      );

      expect(blogPostModelMock.create).toHaveBeenCalledTimes(1);
    });

    it('succeeds when 1080p is incomplete but 2160p is complete', async () => {
      await service.create(
        {
          ...validDto,
          downloadLink: 'magnet:?xt=urn:btih:abc', // 1080p magnet only, no torrent
          downloadLink4k: 'magnet:?xt=urn:btih:abc4k',
          downloadLink4kTorrent: 'https://example.com/file4k.torrent',
        },
        'aaaaaaaaaaaaaaaaaaaaaaa1',
      );

      expect(blogPostModelMock.create).toHaveBeenCalledTimes(1);
    });

    it('succeeds when 2160p is incomplete but 1080p is complete', async () => {
      await service.create(
        {
          ...validDto,
          downloadLink: 'magnet:?xt=urn:btih:abc',
          downloadLinkTorrent: 'https://example.com/file.torrent',
          downloadLink4k: 'magnet:?xt=urn:btih:abc4k', // 4k magnet only, no torrent
        },
        'aaaaaaaaaaaaaaaaaaaaaaa1',
      );

      expect(blogPostModelMock.create).toHaveBeenCalledTimes(1);
    });
  });

  // ==========================================================================
  // create
  // ==========================================================================

  describe('create', () => {
    const dto = {
      title: 'My Post',
      subtitle: 'Episode 1',
      description: 'A great episode',
      thumbnail: 'https://example.com/thumb.jpg',
      downloadLink: 'magnet:?xt=urn:btih:abc',
      downloadLinkTorrent: 'https://example.com/file.torrent',
      dateTimeCreated: '2025-01-01T00:00:00.000Z',
    };

    it('creates a blog post with correct data and sets creator ObjectId', async () => {
      const createdDoc = {
        _id: new Types.ObjectId('dddddddddddddddddddddddd'),
        ...dto,
      };
      blogPostModelMock.create.mockResolvedValue(createdDoc);
      userServiceMock.addCreatedBlogPost.mockResolvedValue(undefined);

      const creatorId = 'aaaaaaaaaaaaaaaaaaaaaa99';
      const result = await service.create(dto, creatorId);

      expect(blogPostModelMock.create).toHaveBeenCalledWith({
        ...dto,
        creator: expect.any(Types.ObjectId),
      });

      const callArg = blogPostModelMock.create.mock.calls[0][0];
      expect(callArg.creator.toString()).toBe(creatorId);

      expect(result).toEqual({ id: createdDoc._id.toString() });
    });

    it('calls userService.addCreatedBlogPost with correct arguments', async () => {
      const createdDoc = {
        _id: new Types.ObjectId('eeeeeeeeeeeeeeeeeeeeeeee'),
      };
      blogPostModelMock.create.mockResolvedValue(createdDoc);
      userServiceMock.addCreatedBlogPost.mockResolvedValue(undefined);

      const creatorId = 'aaaaaaaaaaaaaaaaaaaaaa42';
      await service.create(dto, creatorId);

      expect(userServiceMock.addCreatedBlogPost).toHaveBeenCalledWith(creatorId, createdDoc._id.toString());
    });

    it('throws on invalid resolution (validated indirectly)', async () => {
      await expect(service.create({ ...dto, downloadLinkTorrent: undefined }, 'c-1')).rejects.toThrow(BadRequestException);

      // Model should not have been called
      expect(blogPostModelMock.create).not.toHaveBeenCalled();
    });
  });

  // ==========================================================================
  // findAll
  // ==========================================================================

  describe('findAll', () => {
    it('returns posts and count with default pagination (page 0, pagesize 10)', async () => {
      const posts = [stubPost({ title: 'P1' }), stubPost({ title: 'P2' })];
      const query = makeThenableQuery(posts);
      blogPostModelMock.find.mockReturnValue(query);
      blogPostModelMock.countDocuments.mockResolvedValue(42);

      const result = await service.findAll(10, 0);

      expect(result.posts).toHaveLength(2);
      expect(result.count).toBe(42);
    });

    it('applies correct skip and limit for page=2, pagesize=10', async () => {
      const query = makeThenableQuery([]);
      blogPostModelMock.find.mockReturnValue(query);
      blogPostModelMock.countDocuments.mockResolvedValue(0);

      await service.findAll(10, 2);

      expect(query.skip).toHaveBeenCalledWith(20);
      expect(query.limit).toHaveBeenCalledWith(10);
    });

    it('applies correct skip and limit for page=1, pagesize=50', async () => {
      const query = makeThenableQuery([]);
      blogPostModelMock.find.mockReturnValue(query);
      blogPostModelMock.countDocuments.mockResolvedValue(0);

      await service.findAll(50, 1);

      expect(query.skip).toHaveBeenCalledWith(50);
      expect(query.limit).toHaveBeenCalledWith(50);
    });

    it('populates creator and updatedBy fields', async () => {
      const query = makeThenableQuery([]);
      blogPostModelMock.find.mockReturnValue(query);
      blogPostModelMock.countDocuments.mockResolvedValue(0);

      await service.findAll(10, 0);

      expect(query.populate).toHaveBeenCalledWith('creator', 'username avatar');
      expect(query.populate).toHaveBeenCalledWith('updatedBy', 'username avatar');
    });

    it('sorts by dateTimeCreated descending', async () => {
      const query = makeThenableQuery([]);
      blogPostModelMock.find.mockReturnValue(query);
      blogPostModelMock.countDocuments.mockResolvedValue(0);

      await service.findAll(10, 0);

      expect(query.sort).toHaveBeenCalledWith({ dateTimeCreated: 'desc', _id: 'desc' });
    });

    it('sets updatedAt to undefined on posts without updatedBy (sanitizeEditedMetadata)', async () => {
      const postWithoutEditor = stubPost({
        title: 'No Editor',
        updatedBy: undefined,
        updatedAt: new Date('2025-06-01') as unknown as Date,
      });
      const query = makeThenableQuery([postWithoutEditor]);
      blogPostModelMock.find.mockReturnValue(query);
      blogPostModelMock.countDocuments.mockResolvedValue(1);

      const result = await service.findAll(10, 0);

      expect(result.posts[0].updatedAt).toBeUndefined();
    });

    it('preserves updatedAt when updatedBy exists', async () => {
      const updatedDate = new Date('2025-06-01');
      const postWithEditor = stubPost({
        title: 'Has Editor',
        updatedBy: new Types.ObjectId('ffffffffffffffffffffffff'),
        updatedAt: updatedDate as unknown as Date,
      });
      const query = makeThenableQuery([postWithEditor]);
      blogPostModelMock.find.mockReturnValue(query);
      blogPostModelMock.countDocuments.mockResolvedValue(1);

      const result = await service.findAll(10, 0);

      expect(result.posts[0].updatedAt).toBe(updatedDate);
    });
  });

  // ==========================================================================
  // findOne
  // ==========================================================================

  describe('findOne', () => {
    it('returns the post when found (with populated fields)', async () => {
      const post = stubPost({ title: 'Found Post' });
      blogPostModelMock.findOne.mockReturnValue(makeThenableQuery(post));

      const result = await service.findOne('aaaaaaaaaaaaaaaaaaaaaaaa');

      expect(blogPostModelMock.findOne).toHaveBeenCalledWith({ _id: 'aaaaaaaaaaaaaaaaaaaaaaaa' });
      expect(result).toBeDefined();
      expect(result!.title).toBe('Found Post');
    });

    it('throws NotFoundException when post is not found', async () => {
      blogPostModelMock.findOne.mockReturnValue(makeThenableQuery(null));

      await expect(service.findOne('nonexistent')).rejects.toThrow(NotFoundException);
    });

    it('strips updatedAt when post has no updatedBy', async () => {
      const post = stubPost({
        updatedBy: undefined,
        updatedAt: new Date('2025-06-01') as unknown as Date,
      });
      blogPostModelMock.findOne.mockReturnValue(makeThenableQuery(post));

      const result = await service.findOne('aaaaaaaaaaaaaaaaaaaaaaaa');

      expect(result!.updatedAt).toBeUndefined();
    });

    it('preserves updatedAt when post has updatedBy', async () => {
      const updatedDate = new Date('2025-06-01');
      const post = stubPost({
        updatedBy: new Types.ObjectId('ffffffffffffffffffffffff'),
        updatedAt: updatedDate as unknown as Date,
      });
      blogPostModelMock.findOne.mockReturnValue(makeThenableQuery(post));

      const result = await service.findOne('aaaaaaaaaaaaaaaaaaaaaaaa');

      expect(result!.updatedAt).toBe(updatedDate);
    });
  });

  // ==========================================================================
  // update
  // ==========================================================================

  describe('update', () => {
    const postId = 'aaaaaaaaaaaaaaaaaaaaaaaa';
    const actorId = '000000000000000000000123';

    // Base existing document with a valid 1080p resolution pair so
    // that the merge-based validation in `update()` passes by default.
    const existingDoc = {
      _id: new Types.ObjectId(postId),
      thumbnail: 'https://example.com/old-thumb.jpg',
      downloadLink: 'magnet:?xt=urn:btih:existing',
      downloadLinkTorrent: 'https://example.com/existing.torrent',
      downloadLink4k: undefined,
      downloadLink4kTorrent: undefined,
      toObject() {
        return {
          thumbnail: this.thumbnail,
          downloadLink: this.downloadLink,
          downloadLinkTorrent: this.downloadLinkTorrent,
          downloadLink4k: this.downloadLink4k,
          downloadLink4kTorrent: this.downloadLink4kTorrent,
        };
      },
    };

    beforeEach(() => {
      mediaServiceMock.deleteManagedImageByUrl.mockResolvedValue(undefined);
    });

    it('throws NotFoundException when post does not exist', async () => {
      blogPostModelMock.findById.mockReturnValue(makeThenableQuery(null));

      await expect(service.update(postId, {}, actorId)).rejects.toThrow(NotFoundException);
      expect(blogPostModelMock.findByIdAndUpdate).not.toHaveBeenCalled();
    });

    it('merges existing + incoming resolution fields before validation', async () => {
      // Existing post has a complete 1080p pair; incoming DTO is empty.
      const existing = {
        ...existingDoc,
        downloadLink: 'magnet:?xt=urn:btih:old',
        downloadLinkTorrent: 'https://example.com/old.torrent',
        toObject() {
          return {
            thumbnail: this.thumbnail,
            downloadLink: this.downloadLink,
            downloadLinkTorrent: this.downloadLinkTorrent,
            downloadLink4k: this.downloadLink4k,
            downloadLink4kTorrent: this.downloadLink4kTorrent,
          };
        },
      };

      blogPostModelMock.findById.mockReturnValue(makeThenableQuery(existing));
      blogPostModelMock.findByIdAndUpdate.mockReturnValue(makeThenableQuery(existing));

      await service.update(postId, {}, actorId);

      // No BadRequestException → merge passed validation
      expect(blogPostModelMock.findByIdAndUpdate).toHaveBeenCalledTimes(1);
    });

    it('throws BadRequestException when merged resolution is still incomplete', async () => {
      // Existing post has only a magnet (incomplete 1080p). Incoming adds nothing.
      const existing = {
        ...existingDoc,
        downloadLink: 'magnet:?xt=urn:btih:partial',
        downloadLinkTorrent: undefined,
        toObject() {
          return {
            thumbnail: this.thumbnail,
            downloadLink: this.downloadLink,
            downloadLinkTorrent: this.downloadLinkTorrent,
            downloadLink4k: this.downloadLink4k,
            downloadLink4kTorrent: this.downloadLink4kTorrent,
          };
        },
      };

      blogPostModelMock.findById.mockReturnValue(makeThenableQuery(existing));

      await expect(service.update(postId, {}, actorId)).rejects.toThrow(BadRequestException);
      expect(blogPostModelMock.findByIdAndUpdate).not.toHaveBeenCalled();
    });

    it('succeeds when 1080p was incomplete but 2160p is now provided', async () => {
      // Incoming provides a complete 4k pair
      const existing = {
        ...existingDoc,
        downloadLink: 'magnet:?xt=urn:btih:partial-1080',
        toObject() {
          return {
            thumbnail: this.thumbnail,
            downloadLink: this.downloadLink,
            downloadLinkTorrent: this.downloadLinkTorrent,
            downloadLink4k: this.downloadLink4k,
            downloadLink4kTorrent: this.downloadLink4kTorrent,
          };
        },
      };

      blogPostModelMock.findById.mockReturnValue(makeThenableQuery(existing));
      blogPostModelMock.findByIdAndUpdate.mockReturnValue(makeThenableQuery(existing));

      await service.update(
        postId,
        {
          downloadLink4k: 'magnet:?xt=urn:btih:4kfull',
          downloadLink4kTorrent: 'https://example.com/4k.torrent',
        },
        actorId,
      );

      expect(blogPostModelMock.findByIdAndUpdate).toHaveBeenCalledTimes(1);
    });

    it('updates document with new data and sets updatedBy', async () => {
      blogPostModelMock.findById.mockReturnValue(makeThenableQuery(existingDoc));
      blogPostModelMock.findByIdAndUpdate.mockReturnValue(makeThenableQuery(existingDoc));

      await service.update(postId, { title: 'New Title' }, actorId);

      expect(blogPostModelMock.findByIdAndUpdate).toHaveBeenCalledWith(
        postId,
        { title: 'New Title', updatedBy: expect.any(Types.ObjectId) },
        { new: true, runValidators: true },
      );

      const callArg = blogPostModelMock.findByIdAndUpdate.mock.calls[0][1];
      expect(callArg.updatedBy.toString()).toBe(actorId);
    });

    it('cleans up old thumbnail when thumbnail changed', async () => {
      const updatedPost = {
        ...existingDoc,
        thumbnail: 'https://example.com/new-thumb.jpg',
        toObject() {
          return { ...this };
        },
      };

      blogPostModelMock.findById.mockReturnValue(makeThenableQuery(existingDoc));
      blogPostModelMock.findByIdAndUpdate.mockReturnValue(makeThenableQuery(updatedPost));

      await service.update(postId, { thumbnail: 'https://example.com/new-thumb.jpg' }, actorId);

      expect(mediaServiceMock.deleteManagedImageByUrl).toHaveBeenCalledWith('https://example.com/old-thumb.jpg');
    });

    it('does NOT clean up old thumbnail when thumbnail is unchanged', async () => {
      blogPostModelMock.findById.mockReturnValue(makeThenableQuery(existingDoc));
      blogPostModelMock.findByIdAndUpdate.mockReturnValue(makeThenableQuery(existingDoc));

      await service.update(postId, { title: 'Only title changes' }, actorId);

      expect(mediaServiceMock.deleteManagedImageByUrl).not.toHaveBeenCalled();
    });

    it('returns the updated post', async () => {
      const updatedPost = stubPost({ title: 'Updated', thumbnail: 'https://example.com/thumb.jpg' });
      blogPostModelMock.findById.mockReturnValue(makeThenableQuery(existingDoc));
      blogPostModelMock.findByIdAndUpdate.mockReturnValue(makeThenableQuery(updatedPost));

      const result = await service.update(postId, { title: 'Updated' }, actorId);

      expect(result).toBeDefined();
      expect(result!.title).toBe('Updated');
    });
  });

  // ==========================================================================
  // delete
  // ==========================================================================

  describe('delete', () => {
    const postId = 'aaaaaaaaaaaaaaaaaaaaaaaa';
    const actorId = '000000000000000000000999';

    beforeEach(() => {
      mediaServiceMock.deleteManagedImageByUrl.mockResolvedValue(undefined);
      userServiceMock.removeCreatedBlogPost.mockResolvedValue(undefined);
    });

    it('deletes the post and returns it', async () => {
      const deletedPost = stubPost({ title: 'To Be Deleted' });
      blogPostModelMock.findByIdAndDelete.mockReturnValue(makeThenableQuery(deletedPost));

      const result = await service.delete(postId, actorId);

      expect(blogPostModelMock.findByIdAndDelete).toHaveBeenCalledWith(postId);
      expect(result).toBeDefined();
      expect(result.title).toBe('To Be Deleted');
    });

    it('cleans up thumbnail via mediaService', async () => {
      const deletedPost = stubPost({ thumbnail: 'https://example.com/to-delete.jpg' });
      blogPostModelMock.findByIdAndDelete.mockReturnValue(makeThenableQuery(deletedPost));

      await service.delete(postId, actorId);

      expect(mediaServiceMock.deleteManagedImageByUrl).toHaveBeenCalledWith('https://example.com/to-delete.jpg');
    });

    it('removes post from user created list via userService', async () => {
      const deletedPost = stubPost({
        creator: new Types.ObjectId('cccccccccccccccccccccccc'),
      });
      blogPostModelMock.findByIdAndDelete.mockReturnValue(makeThenableQuery(deletedPost));

      await service.delete(postId, actorId);

      expect(userServiceMock.removeCreatedBlogPost).toHaveBeenCalledWith('cccccccccccccccccccccccc', postId);
    });

    it('throws NotFoundException when post does not exist', async () => {
      blogPostModelMock.findByIdAndDelete.mockReturnValue(makeThenableQuery(null));

      await expect(service.delete(postId, actorId)).rejects.toThrow(NotFoundException);
      expect(mediaServiceMock.deleteManagedImageByUrl).not.toHaveBeenCalled();
      expect(userServiceMock.removeCreatedBlogPost).not.toHaveBeenCalled();
    });

    it('handles creator as ObjectId and converts to string', async () => {
      const creatorId = new Types.ObjectId('bbbbbbbbbbbbbbbbbbbbbbbb');
      const deletedPost = stubPost({ creator: creatorId });
      blogPostModelMock.findByIdAndDelete.mockReturnValue(makeThenableQuery(deletedPost));

      await service.delete(postId, actorId);

      expect(userServiceMock.removeCreatedBlogPost).toHaveBeenCalledWith(creatorId.toString(), postId);
    });
  });
});
