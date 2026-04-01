import { BlogPost } from '@shared/types';

export type AccountViewState = 'loading' | 'error' | 'ready' | 'idle';

export type FavoritesViewState =
  | 'loading-ids'
  | 'ids-error'
  | 'empty-ids'
  | 'loading-posts'
  | 'posts-error'
  | 'empty-posts'
  | 'ready';

export interface FavoritesPageChange {
  page: number;
  pageSize: number;
}

export interface UserProfile {
  id: string;
  username: string;
  email: string;
  avatar?: string;
  isAdmin: boolean;
  favoriteBlogPostIds: string[];
  createdBlogPostIds: string[];
  createdAt?: string;
  updatedAt?: string;
}

export interface FavoriteBlogPostIdsResponse {
  favoriteBlogPostIds: string[];
}

export interface FavoriteBlogPostsResponse {
  posts: BlogPost[];
  count: number;
}

export interface UpdateUserRequest {
  email?: string;
  avatar?: string;
  password?: string;
}

export interface MediaUploadResponse {
  url: string;
}
