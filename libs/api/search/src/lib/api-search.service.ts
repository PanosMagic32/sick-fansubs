import { BlogPost } from '@api/blog-post';
import { Project } from '@api/project';
import { Injectable } from '@nestjs/common';
import { InjectModel } from '@nestjs/mongoose';
import { Document, Model } from 'mongoose';

import { SearchOptions } from './interfaces/search-options.interface';
import { Searchable } from './interfaces/searchable.interface';

const DEFAULT_PAGE_SIZE = 5;

@Injectable()
export class ApiSearchService {
  constructor(
    @InjectModel(BlogPost.name) private readonly blogPostModel: Model<BlogPost & Document>,
    @InjectModel(Project.name) private readonly projectModel: Model<Project & Document>,
  ) {}

  private async searchModel<T extends Document>(
    model: Model<T>,
    baseQuery: Record<string, any>,
    sort: any,
    modelType: string,
    skip: number,
    limit: number,
  ): Promise<{ results: any[]; count: number }> {
    const sortStage = sort ? { $sort: sort } : { $sort: { dateTimeCreated: -1 } };

    const pipeline: any[] = [
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

    const results = await model.aggregate(pipeline).exec();

    // For total count, query without pagination
    const totalCount = await model.countDocuments(baseQuery);

    return {
      results: results as any[],
      count: totalCount,
    };
  }

  async search(options: SearchOptions): Promise<{ results: Searchable[]; total: number }> {
    const { searchTerm, type, filters, pagination, sort } = options;
    const sorting = sort ?? { dateTimeCreated: -1 };

    const normalizedPagination = {
      page: Number(pagination?.page || 0),
      pageSize: Number(pagination?.pageSize || DEFAULT_PAGE_SIZE),
    };

    const skip = normalizedPagination.page * normalizedPagination.pageSize;
    const limit = normalizedPagination.pageSize;

    const baseQuery: Record<string, any> = {
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
    };

    // Execute searches based on type
    const searches: Promise<{ results: any[]; count: number }>[] = [];

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
      const [sortField, sortOrder] = Object.entries(sorting)[0];
      combinedResults.results.sort((a, b) => {
        const aVal = a[sortField];
        const bVal = b[sortField];
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
