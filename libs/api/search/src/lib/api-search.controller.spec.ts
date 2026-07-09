import { vi } from 'vitest';

import { ApiSearchController } from './api-search.controller';

describe('ApiSearchController', () => {
  let controller: ApiSearchController;

  const searchServiceMock = {
    search: vi.fn(),
  };

  beforeEach(() => {
    vi.clearAllMocks();

    controller = new ApiSearchController(searchServiceMock as never);
  });

  // -- sanity --
  it('should be defined', () => {
    expect(controller).toBeDefined();
  });

  // =========================================================================
  // search – DTO mapping
  // =========================================================================

  it('maps all DTO fields to service SearchOptions correctly', async () => {
    searchServiceMock.search.mockResolvedValue({ results: [], total: 0 });

    const dto = {
      searchTerm: 'naruto',
      type: 'project' as const,
      page: 2,
      pageSize: 20,
    };

    await controller.search(dto as never);

    expect(searchServiceMock.search).toHaveBeenCalledWith({
      searchTerm: 'naruto',
      type: 'project',
      pagination: { page: 2, pageSize: 20 },
    });

    expect(searchServiceMock.search).toHaveBeenCalledTimes(1);
  });

  it('maps searchTerm and type as blog-post', async () => {
    searchServiceMock.search.mockResolvedValue({ results: [], total: 0 });

    await controller.search({ searchTerm: 'release', type: 'blog-post', page: 0, pageSize: 10 } as never);

    expect(searchServiceMock.search).toHaveBeenCalledWith({
      searchTerm: 'release',
      type: 'blog-post',
      pagination: { page: 0, pageSize: 10 },
    });
  });

  it('passes undefined values through when DTO properties are missing', async () => {
    searchServiceMock.search.mockResolvedValue({ results: [], total: 0 });

    await controller.search({} as never);

    expect(searchServiceMock.search).toHaveBeenCalledWith({
      searchTerm: undefined,
      type: undefined,
      pagination: { page: undefined, pageSize: undefined },
    });
  });

  it('passes page and pageSize as-is from the DTO', async () => {
    searchServiceMock.search.mockResolvedValue({ results: [], total: 0 });

    await controller.search({ page: 0, pageSize: 5 } as never);

    expect(searchServiceMock.search).toHaveBeenCalledWith(
      expect.objectContaining({
        pagination: { page: 0, pageSize: 5 },
      }),
    );
  });

  it('returns the service result directly', async () => {
    const serviceResult = {
      results: [
        {
          _id: '1',
          title: 'Test',
          description: 'Desc',
          thumbnail: 't.jpg',
          dateTimeCreated: new Date(),
          type: 'blog-post' as const,
        },
      ],
      total: 1,
    };

    searchServiceMock.search.mockResolvedValue(serviceResult);

    const result = await controller.search({ searchTerm: 'Test' } as never);

    expect(result).toBe(serviceResult);
  });
});
