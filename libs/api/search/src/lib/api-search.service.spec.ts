import { vi } from 'vitest';

// ---------------------------------------------------------------------------
// Module mocks — break the deep import chain so NestJS / class-validator
// decorators are never evaluated during isolated tests.
// ---------------------------------------------------------------------------

vi.mock('@api/blog-post', () => ({
  BlogPost: class BlogPost {},
}));

vi.mock('@api/project', () => ({
  Project: class Project {},
}));

vi.mock('@api/shared', () => ({
  escapeRegex: (str: string) => str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'),
}));

// ---------------------------------------------------------------------------
// Imports (after mocks so hoisting picks them up)
// ---------------------------------------------------------------------------

import { ApiSearchService } from './api-search.service';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Builds a mock Mongoose model with chained .aggregate().exec() support. */
function createModelMock() {
  const exec = vi.fn();
  const aggregate = vi.fn().mockReturnValue({ exec });
  const countDocuments = vi.fn();
  return { aggregate, exec, countDocuments };
}

// ---------------------------------------------------------------------------
// Suite
// ---------------------------------------------------------------------------

describe('ApiSearchService', () => {
  let service: ApiSearchService;

  // -- mocks --
  const blogPostMock = createModelMock();
  const projectMock = createModelMock();

  beforeEach(() => {
    vi.clearAllMocks();

    service = new ApiSearchService(blogPostMock as never, projectMock as never);
  });

  // -- sanity --
  it('should be defined', () => {
    expect(service).toBeDefined();
  });

  // =========================================================================
  // toMongoSort (tested indirectly via search)
  // =========================================================================

  describe('toMongoSort (via search)', () => {
    const EMPTY_SORT = { $sort: { dateTimeCreated: -1 } };

    beforeEach(() => {
      blogPostMock.exec.mockResolvedValue([]);
      blogPostMock.countDocuments.mockResolvedValue(0);
    });

    it('maps asc direction to MongoDB 1', async () => {
      await service.search({
        type: 'blog-post',
        sort: { title: 'asc' },
        pagination: { page: 0, pageSize: 5 },
      });

      const pipeline = blogPostMock.aggregate.mock.calls[0][0];
      expect(pipeline[1]).toEqual({ $sort: { title: 1 } });
    });

    it('maps desc direction to MongoDB -1', async () => {
      await service.search({
        type: 'blog-post',
        sort: { title: 'desc' },
        pagination: { page: 0, pageSize: 5 },
      });

      const pipeline = blogPostMock.aggregate.mock.calls[0][0];
      expect(pipeline[1]).toEqual({ $sort: { title: -1 } });
    });

    it('uses default sort when no sort option is provided', async () => {
      await service.search({
        type: 'blog-post',
        pagination: { page: 0, pageSize: 5 },
      });

      const pipeline = blogPostMock.aggregate.mock.calls[0][0];
      expect(pipeline[1]).toEqual(EMPTY_SORT);
    });

    it('uses default sort when sort object is empty', async () => {
      await service.search({
        type: 'blog-post',
        sort: {},
        pagination: { page: 0, pageSize: 5 },
      });

      const pipeline = blogPostMock.aggregate.mock.calls[0][0];
      expect(pipeline[1]).toEqual(EMPTY_SORT);
    });

    it('maps multiple sort fields', async () => {
      await service.search({
        type: 'blog-post',
        sort: { title: 'asc', dateTimeCreated: 'desc' },
        pagination: { page: 0, pageSize: 5 },
      });

      const pipeline = blogPostMock.aggregate.mock.calls[0][0];
      expect(pipeline[1]).toEqual({ $sort: { title: 1, dateTimeCreated: -1 } });
    });
  });

  // =========================================================================
  // search – type dispatch
  // =========================================================================

  describe('search – type dispatch', () => {
    beforeEach(() => {
      blogPostMock.exec.mockResolvedValue([]);
      blogPostMock.countDocuments.mockResolvedValue(0);
      projectMock.exec.mockResolvedValue([]);
      projectMock.countDocuments.mockResolvedValue(0);
    });

    it('searches blog-post type only', async () => {
      await service.search({
        type: 'blog-post',
        pagination: { page: 0, pageSize: 5 },
      });

      expect(blogPostMock.aggregate).toHaveBeenCalledTimes(1);
      expect(projectMock.aggregate).not.toHaveBeenCalled();
    });

    it('searches project type only', async () => {
      await service.search({
        type: 'project',
        pagination: { page: 0, pageSize: 5 },
      });

      expect(projectMock.aggregate).toHaveBeenCalledTimes(1);
      expect(blogPostMock.aggregate).not.toHaveBeenCalled();
    });

    it('searches "all" type (both collections)', async () => {
      await service.search({
        type: 'all',
        pagination: { page: 0, pageSize: 5 },
      });

      expect(blogPostMock.aggregate).toHaveBeenCalledTimes(1);
      expect(projectMock.aggregate).toHaveBeenCalledTimes(1);
    });
  });

  // =========================================================================
  // search – filters
  // =========================================================================

  describe('search – filters', () => {
    beforeEach(() => {
      blogPostMock.exec.mockResolvedValue([]);
      blogPostMock.countDocuments.mockResolvedValue(0);
      projectMock.exec.mockResolvedValue([]);
      projectMock.countDocuments.mockResolvedValue(0);
    });

    it('includes searchTerm as case-insensitive regex across title/description/subtitle', async () => {
      await service.search({
        searchTerm: 'naruto',
        type: 'blog-post',
        pagination: { page: 0, pageSize: 5 },
      });

      const pipeline = blogPostMock.aggregate.mock.calls[0][0];
      const match = pipeline[0].$match as Record<string, unknown>;

      expect(match.$or).toBeDefined();
      const or = match.$or as Array<Record<string, unknown>>;
      expect(or).toHaveLength(3);

      const fields = or.map((clause) => Object.keys(clause)[0]);
      expect(fields).toContain('title');
      expect(fields).toContain('description');
      expect(fields).toContain('subtitle');

      for (const clause of or) {
        const inner = Object.values(clause)[0] as Record<string, unknown>;
        expect(inner.$regex).toBe('naruto');
        expect(inner.$options).toBe('i');
      }
    });

    it('escapes regex-special characters in searchTerm', async () => {
      await service.search({
        searchTerm: 'test.regex+',
        type: 'blog-post',
        pagination: { page: 0, pageSize: 5 },
      });

      const pipeline = blogPostMock.aggregate.mock.calls[0][0];
      const match = pipeline[0].$match as Record<string, unknown>;
      const or = match.$or as Array<Record<string, unknown>>;

      // escapeRegex should escape . and + → \. \+
      for (const clause of or) {
        const inner = Object.values(clause)[0] as Record<string, unknown>;
        expect(inner.$regex).toBe('test\\.regex\\+');
      }
    });

    it('omits $or filter when searchTerm is missing (returns all)', async () => {
      await service.search({
        type: 'blog-post',
        pagination: { page: 0, pageSize: 5 },
      });

      const pipeline = blogPostMock.aggregate.mock.calls[0][0];
      const match = pipeline[0].$match as Record<string, unknown>;

      expect(match.$or).toBeUndefined();
    });

    it('omits $or filter when searchTerm is empty string', async () => {
      await service.search({
        searchTerm: '',
        type: 'blog-post',
        pagination: { page: 0, pageSize: 5 },
      });

      const pipeline = blogPostMock.aggregate.mock.calls[0][0];
      const match = pipeline[0].$match as Record<string, unknown>;

      expect(match.$or).toBeUndefined();
    });

    it('applies dateFrom filter', async () => {
      const dateFrom = new Date('2024-06-01');

      await service.search({
        type: 'blog-post',
        filters: { dateFrom },
        pagination: { page: 0, pageSize: 5 },
      });

      const pipeline = blogPostMock.aggregate.mock.calls[0][0];
      const match = pipeline[0].$match as Record<string, unknown>;
      const dt = match.dateTimeCreated as Record<string, unknown>;

      expect(dt.$gte).toEqual(dateFrom);
      expect(dt.$lte).toBeUndefined();
    });

    it('applies dateTo filter', async () => {
      const dateTo = new Date('2024-12-31');

      await service.search({
        type: 'blog-post',
        filters: { dateTo },
        pagination: { page: 0, pageSize: 5 },
      });

      const pipeline = blogPostMock.aggregate.mock.calls[0][0];
      const match = pipeline[0].$match as Record<string, unknown>;
      const dt = match.dateTimeCreated as Record<string, unknown>;

      expect(dt.$lte).toEqual(dateTo);
      expect(dt.$gte).toBeUndefined();
    });

    it('applies both dateFrom and dateTo filters together', async () => {
      const dateFrom = new Date('2024-01-01');
      const dateTo = new Date('2024-12-31');

      await service.search({
        type: 'blog-post',
        filters: { dateFrom, dateTo },
        pagination: { page: 0, pageSize: 5 },
      });

      const pipeline = blogPostMock.aggregate.mock.calls[0][0];
      const match = pipeline[0].$match as Record<string, unknown>;
      const dt = match.dateTimeCreated as Record<string, unknown>;

      expect(dt.$gte).toEqual(dateFrom);
      expect(dt.$lte).toEqual(dateTo);
    });
  });

  // =========================================================================
  // search – pagination
  // =========================================================================

  describe('search – pagination', () => {
    beforeEach(() => {
      blogPostMock.exec.mockResolvedValue([]);
      blogPostMock.countDocuments.mockResolvedValue(0);
    });

    it('applies default pageSize when pagination is missing pageSize', async () => {
      await service.search({
        type: 'blog-post',
        pagination: {} as never,
      });

      const pipeline = blogPostMock.aggregate.mock.calls[0][0];
      expect(pipeline[2]).toEqual({ $skip: 0 });
      expect(pipeline[3]).toEqual({ $limit: 5 }); // DEFAULT_PAGE_SIZE
    });

    it('computes skip from page * pageSize', async () => {
      await service.search({
        type: 'blog-post',
        pagination: { page: 3, pageSize: 10 },
      });

      const pipeline = blogPostMock.aggregate.mock.calls[0][0];
      expect(pipeline[2]).toEqual({ $skip: 30 });
      expect(pipeline[3]).toEqual({ $limit: 10 });
    });

    it('uses page 0 when page is omitted', async () => {
      await service.search({
        type: 'blog-post',
        pagination: { pageSize: 7 } as never,
      });

      const pipeline = blogPostMock.aggregate.mock.calls[0][0];
      expect(pipeline[2]).toEqual({ $skip: 0 });
      expect(pipeline[3]).toEqual({ $limit: 7 });
    });

    it('defaults pageSize to DEFAULT_PAGE_SIZE when not provided', async () => {
      await service.search({
        type: 'blog-post',
        pagination: { page: 1 } as never,
      });

      const pipeline = blogPostMock.aggregate.mock.calls[0][0];
      expect(pipeline[2]).toEqual({ $skip: 5 });
      expect(pipeline[3]).toEqual({ $limit: 5 });
    });
  });

  // =========================================================================
  // search – result behaviour
  // =========================================================================

  describe('search – result behaviour', () => {
    it('returns empty results when nothing matches', async () => {
      blogPostMock.exec.mockResolvedValue([]);
      blogPostMock.countDocuments.mockResolvedValue(0);

      const result = await service.search({
        searchTerm: 'nonexistent',
        type: 'blog-post',
        pagination: { page: 0, pageSize: 5 },
      });

      expect(result.results).toEqual([]);
      expect(result.total).toBe(0);
    });

    it('returns results with correct structure from a single collection', async () => {
      const documents = [
        {
          _id: '1',
          title: 'Post 1',
          description: 'Desc 1',
          subtitle: 'Sub 1',
          thumbnail: 't.jpg',
          dateTimeCreated: new Date(),
          downloadLink: 'dl',
          downloadLink4k: 'dl4k',
          type: 'blog-post',
        },
        {
          _id: '2',
          title: 'Post 2',
          description: 'Desc 2',
          subtitle: 'Sub 2',
          thumbnail: 't2.jpg',
          dateTimeCreated: new Date(),
          downloadLink: 'dl2',
          downloadLink4k: 'dl4k2',
          type: 'blog-post',
        },
      ];

      blogPostMock.exec.mockResolvedValue(documents);
      blogPostMock.countDocuments.mockResolvedValue(2);

      const result = await service.search({
        type: 'blog-post',
        pagination: { page: 0, pageSize: 5 },
      });

      expect(result.results).toEqual(documents);
      expect(result.total).toBe(2);
    });

    it('merges results from both collections for "all" type', async () => {
      const blogResults = [{ _id: 'bp1', title: 'Blog 1', dateTimeCreated: new Date('2024-01-01'), type: 'blog-post' }];
      const projectResults = [{ _id: 'p1', title: 'Project 1', dateTimeCreated: new Date('2024-02-01'), type: 'project' }];

      blogPostMock.exec.mockResolvedValue(blogResults);
      blogPostMock.countDocuments.mockResolvedValue(1);
      projectMock.exec.mockResolvedValue(projectResults);
      projectMock.countDocuments.mockResolvedValue(1);

      const result = await service.search({
        type: 'all',
        pagination: { page: 0, pageSize: 5 },
      });

      expect(result.results).toHaveLength(2);
      expect(result.total).toBe(2);
    });

    it('re-sorts combined results by sort field for "all" type (desc)', async () => {
      const blogResults = [
        { _id: 'bp1', title: 'Blog 1', dateTimeCreated: new Date('2024-01-01'), type: 'blog-post' },
        { _id: 'bp2', title: 'Blog 2', dateTimeCreated: new Date('2024-03-01'), type: 'blog-post' },
      ];
      const projectResults = [{ _id: 'p1', title: 'Project 1', dateTimeCreated: new Date('2024-02-01'), type: 'project' }];

      blogPostMock.exec.mockResolvedValue(blogResults);
      blogPostMock.countDocuments.mockResolvedValue(2);
      projectMock.exec.mockResolvedValue(projectResults);
      projectMock.countDocuments.mockResolvedValue(1);

      const result = await service.search({
        type: 'all',
        sort: { dateTimeCreated: 'desc' },
        pagination: { page: 0, pageSize: 5 },
      });

      // Expected order desc: bp2 (Mar) → p1 (Feb) → bp1 (Jan)
      expect(result.results).toHaveLength(3);
      expect(result.results[0]._id).toBe('bp2');
      expect(result.results[1]._id).toBe('p1');
      expect(result.results[2]._id).toBe('bp1');
      expect(result.total).toBe(3);
    });

    it('re-sorts combined results by sort field for "all" type (asc)', async () => {
      const blogResults = [{ _id: 'bp2', title: 'Blog 2', dateTimeCreated: new Date('2024-03-01'), type: 'blog-post' }];
      const projectResults = [{ _id: 'p1', title: 'Project 1', dateTimeCreated: new Date('2024-01-01'), type: 'project' }];

      blogPostMock.exec.mockResolvedValue(blogResults);
      blogPostMock.countDocuments.mockResolvedValue(1);
      projectMock.exec.mockResolvedValue(projectResults);
      projectMock.countDocuments.mockResolvedValue(1);

      const result = await service.search({
        type: 'all',
        sort: { dateTimeCreated: 'asc' },
        pagination: { page: 0, pageSize: 5 },
      });

      // Expected order asc: p1 (Jan) → bp2 (Mar)
      expect(result.results).toHaveLength(2);
      expect(result.results[0]._id).toBe('p1');
      expect(result.results[1]._id).toBe('bp2');
      expect(result.total).toBe(2);
    });

    it('truncates combined results to pageSize for "all" type', async () => {
      const blogResults = Array.from({ length: 4 }, (_, i) => ({
        _id: `bp${i}`,
        title: `Blog ${i}`,
        dateTimeCreated: new Date(2024, 0, i + 1),
        type: 'blog-post' as const,
      }));
      const projectResults = Array.from({ length: 4 }, (_, i) => ({
        _id: `p${i}`,
        title: `Project ${i}`,
        dateTimeCreated: new Date(2024, 0, i + 10),
        type: 'project' as const,
      }));

      blogPostMock.exec.mockResolvedValue(blogResults);
      blogPostMock.countDocuments.mockResolvedValue(4);
      projectMock.exec.mockResolvedValue(projectResults);
      projectMock.countDocuments.mockResolvedValue(4);

      const result = await service.search({
        type: 'all',
        pagination: { page: 0, pageSize: 5 },
      });

      expect(result.results).toHaveLength(5);
      // total reflects the real sum, not the slice
      expect(result.total).toBe(8);
    });
  });
});
