export interface SearchOptions {
  searchTerm?: string;
  type?: 'blog-post' | 'project' | 'all';
  filters?: {
    tags?: string[];
    dateFrom?: Date;
    dateTo?: Date;
  };
  pagination: {
    page: number;
    pageSize: number;
  };
  sort?: { [key: string]: 'asc' | 'desc' };
}
