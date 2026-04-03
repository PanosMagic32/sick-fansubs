import { BlogPost } from '@api/blog-post';
import { Project } from '@api/project';
import { Injectable } from '@nestjs/common';
import { InjectModel } from '@nestjs/mongoose';
import { Document, type FilterQuery, Model, type PipelineStage } from 'mongoose';

import { SearchOptions } from './interfaces/search-options.interface';
import { Searchable } from './interfaces/searchable.interface';

const DEFAULT_PAGE_SIZE = 5;

@Injectable()
export class ApiSearchService {
  constructor(
    @InjectModel(BlogPost.name) private readonly blogPostModel: Model<BlogPost & Document>,
    @InjectModel(Project.name) private readonly projectModel: Model<Project & Document>,
  ) {}

  private toMongoSort(sort?: SearchOptions['sort']): Record<string, 1 | -1> {
    if (!sort) {
      return { dateTimeCreated: -1 };
    }

    const mappedEntries = Object.entries(sort).map(([field, direction]) => [field, direction === 'asc' ? 1 : -1] as const);

    if (mappedEntries.length === 0) {
      return { dateTimeCreated: -1 };
    }

    return Object.fromEntries(mappedEntries);
  }

  private async searchModel<T extends Document>(
    model: Model<T>,
    baseQuery: FilterQuery<T>,
    sort: Record<string, 1 | -1>,
    modelType: Searchable['type'],
    skip: number,
    limit: number,
  ): Promise<{ results: Searchable[]; count: number }> {
    const sortStage: PipelineStage.Sort = { $sort: sort };

    const pipeline: PipelineStage[] = [
      { $match: baseQuery },
      sortStage,
      { $skip: skip },
      { $limit: limit },
      {
        $project: {
          _id: 1,
          title: 1,
          description: 1,
          subtitle: 1,
          thumbnail: 1,
          dateTimeCreated: 1,
          downloadLink: 1,
          downloadLink4k: 1,
          type: { $literal: modelType },
        },
      },
    ];

    const results = await model.aggregate<Searchable>(pipeline).exec();

    // For total count, query without pagination
    const totalCount = await model.countDocuments(baseQuery);

    return {
      results,
      count: totalCount,
    };
  }

  async search(options: SearchOptions): Promise<{ results: Searchable[]; total: number }> {
    const { searchTerm, type, filters, pagination, sort } = options;
    const sorting = this.toMongoSort(sort);

    const normalizedPagination = {
      page: Number(pagination?.page || 0),
      pageSize: Number(pagination?.pageSize || DEFAULT_PAGE_SIZE),
    };

    const skip = normalizedPagination.page * normalizedPagination.pageSize;
    const limit = normalizedPagination.pageSize;

    const baseQuery = {
      ...(searchTerm && {
        $or: [
          { title: { $regex: searchTerm, $options: 'i' } },
          { description: { $regex: searchTerm, $options: 'i' } },
          { subtitle: { $regex: searchTerm, $options: 'i' } },
        ],
      }),
      ...(filters?.dateFrom || filters?.dateTo
        ? {
            dateTimeCreated: {
              ...(filters.dateFrom && { $gte: filters.dateFrom }),
              ...(filters.dateTo && { $lte: filters.dateTo }),
            },
          }
        : {}),
    } as const;

    // Execute searches based on type
    const searches: Promise<{ results: Searchable[]; count: number }>[] = [];

    if (type === 'all' || type === 'blog-post') {
      searches.push(this.searchModel(this.blogPostModel, baseQuery, sorting, 'blog-post', skip, limit));
    }

    if (type === 'all' || type === 'project') {
      searches.push(this.searchModel(this.projectModel, baseQuery, sorting, 'project', skip, limit));
    }

    const searchResults = await Promise.all(searches);

    // Combine results
    const combinedResults = searchResults.reduce(
      (acc, curr) => ({
        results: [...acc.results, ...curr.results],
        total: acc.total + curr.count,
      }),
      { results: [], total: 0 },
    );

    // For 'all' type, re-sort combined results to account for cross-collection sorting
    if (type === 'all' && sorting) {
      const [sortField, sortOrder] = Object.entries(sorting)[0] as [string, 1 | -1] | undefined;

      if (!sortField) {
        return combinedResults;
      }

      combinedResults.results.sort((a, b) => {
        const aVal = a[sortField as keyof Searchable];
        const bVal = b[sortField as keyof Searchable];
        if (aVal === bVal) return 0;
        if (sortOrder === -1) {
          return aVal < bVal ? 1 : -1;
        } else {
          return aVal > bVal ? 1 : -1;
        }
      });
    }

    // For 'all' type, we may have more results than needed due to fetching from both collections
    // Slice to ensure we return exactly pageSize results if available
    if (type === 'all' && combinedResults.results.length > limit) {
      combinedResults.results = combinedResults.results.slice(0, limit);
    }

    return combinedResults;
  }
}
