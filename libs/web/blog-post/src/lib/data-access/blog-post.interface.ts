import type { BlogPost } from '@shared/types';

export type { BlogPost, CreateBlogPost, EditBlogPost } from '@shared/types';

export interface BlogPostResponse {
  posts: BlogPost[];
  count: number;
}
