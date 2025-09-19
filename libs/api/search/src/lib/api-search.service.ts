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
  ): Promise<{ results: any[]; total: number }> {
    const query = model.find(baseQuery);
    if (sort) {
      query.sort(sort);
    }

    const [results, count] = await Promise.all([query.lean().exec(), model.countDocuments(baseQuery)]);

    return {
      results: results.map((result) => ({ ...result, type: modelType })),
      total: count,
    };
  }

  async search(options: SearchOptions): Promise<{ results: Searchable[]; total: number }> {
    const { searchTerm, type, filters, pagination, sort } = options;
    const sorting = sort ?? { dateTimeCreated: -1 };

    const normalizedPagination = {
      page: Number(pagination?.page || 0),
      pageSize: Number(pagination?.pageSize || DEFAULT_PAGE_SIZE),
    };

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
    const searches: Promise<{ results: any[]; total: number }>[] = [];

    if (type === 'all' || type === 'blog-post') {
      searches.push(this.searchModel(this.blogPostModel, baseQuery, sorting, 'blog-post'));
    }

    if (type === 'all' || type === 'project') {
      searches.push(this.searchModel(this.projectModel, baseQuery, sorting, 'project'));
    }

    const searchResults = await Promise.all(searches);

    // Combine results
    const combinedResults = searchResults.reduce(
      (acc, curr) => ({
        results: [...acc.results, ...curr.results],
        total: acc.total + curr.total,
      }),
      { results: [], total: 0 },
    );

    // Apply sorting to combined results if needed
    if (sorting) {
      const [sortField, sortOrder] = Object.entries(sorting)[0];
      combinedResults.results.sort((a, b) => {
        if (sortOrder === 'asc') {
          return a[sortField] > b[sortField] ? 1 : -1;
        } else {
          return a[sortField] < b[sortField] ? 1 : -1;
        }
      });
    }

    // Apply pagination to combined results using normalized pagination values
    const start = normalizedPagination.page * normalizedPagination.pageSize;
    const end = start + normalizedPagination.pageSize;
    combinedResults.results = combinedResults.results.slice(start, end);

    return combinedResults;
  }
}
