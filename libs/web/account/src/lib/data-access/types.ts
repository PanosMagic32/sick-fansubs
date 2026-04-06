import { BlogPost, Project } from '@shared/types';
import type { UserRole, UserStatus } from '@shared/types';

export type AccountViewState = 'loading' | 'error' | 'ready' | 'idle';

export type FavoritesViewState =
  | 'loading-ids'
  | 'ids-error'
  | 'empty-ids'
  | 'loading-posts'
  | 'posts-error'
  | 'empty-posts'
  | 'ready';

export type FavoriteProjectsViewState =
  | 'loading-ids'
  | 'ids-error'
  | 'empty-ids'
  | 'loading-projects'
  | 'projects-error'
  | 'empty-projects'
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
  role?: UserRole;
  status?: UserStatus;
  favoriteBlogPostIds: string[];
  favoriteProjectIds: string[];
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

export interface FavoriteProjectIdsResponse {
  favoriteProjectIds: string[];
}

export interface FavoriteProjectsResponse {
  projects: Project[];
  count: number;
}

export type FavoriteSortOrder = 'newest' | 'oldest';

export interface UpdateUserRequest {
  email?: string;
  avatar?: string;
  password?: string;
}

export interface MediaUploadResponse {
  url: string;
}
