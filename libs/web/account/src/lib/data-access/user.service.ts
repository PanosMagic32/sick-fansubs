import { httpResource, HttpResourceRef } from '@angular/common/http';
import { inject, Injectable, Signal, WritableSignal } from '@angular/core';

import type { BlogPost } from '@shared/types';
import { WebConfigService } from '@web/shared';

export interface UserProfile {
  id: string;
  username: string;
  email: string;
  avatar?: string;
  isAdmin: boolean;
  favoriteBlogPostIds: string[];
  createdAt?: string;
  updatedAt?: string;
}

export interface FavoriteBlogPostIdsResponse {
  favoriteBlogPostIds: string[];
}

export interface FavoriteBlogPostsResponse {
  posts: BlogPost[];
}

export interface UpdateUserRequest {
  email?: string;
  avatar?: string;
  password?: string;
}

@Injectable({ providedIn: 'root' })
export class UserService {
  private readonly webConfigService = inject(WebConfigService);

  getUserProfile(userId: Signal<string>): HttpResourceRef<UserProfile | undefined> {
    return httpResource<UserProfile>(() => {
      const id = userId();
      if (!id) return;

      return {
        url: `${this.webConfigService.API_URL}/user/${id}`,
      };
    });
  }

  updateUserProfile(
    userId: Signal<string>,
    data: WritableSignal<UpdateUserRequest | null>,
  ): HttpResourceRef<UserProfile | undefined> {
    return httpResource<UserProfile>(() => {
      const id = userId();
      const body = data();
      if (!id || !body) return;

      return {
        url: `${this.webConfigService.API_URL}/user/${id}`,
        method: 'PATCH',
        body,
      };
    });
  }

  getFavoriteBlogPostIds(userId: Signal<string>): HttpResourceRef<FavoriteBlogPostIdsResponse | undefined> {
    return httpResource<FavoriteBlogPostIdsResponse>(() => {
      const id = userId();
      if (!id) return;

      return {
        url: `${this.webConfigService.API_URL}/user/${id}/favorites`,
      };
    });
  }

  addFavoriteBlogPost(
    userId: Signal<string>,
    postId: WritableSignal<string | null>,
  ): HttpResourceRef<FavoriteBlogPostIdsResponse | undefined> {
    return httpResource<FavoriteBlogPostIdsResponse>(() => {
      const id = userId();
      const favoritePostId = postId();
      if (!id || !favoritePostId) return;

      return {
        url: `${this.webConfigService.API_URL}/user/${id}/favorites/${favoritePostId}`,
        method: 'PUT',
      };
    });
  }

  removeFavoriteBlogPost(
    userId: Signal<string>,
    postId: WritableSignal<string | null>,
  ): HttpResourceRef<FavoriteBlogPostIdsResponse | undefined> {
    return httpResource<FavoriteBlogPostIdsResponse>(() => {
      const id = userId();
      const favoritePostId = postId();
      if (!id || !favoritePostId) return;

      return {
        url: `${this.webConfigService.API_URL}/user/${id}/favorites/${favoritePostId}`,
        method: 'DELETE',
      };
    });
  }

  getFavoriteBlogPosts(userId: Signal<string>): HttpResourceRef<FavoriteBlogPostsResponse | undefined> {
    return httpResource<FavoriteBlogPostsResponse>(() => {
      const id = userId();
      if (!id) return;

      return {
        url: `${this.webConfigService.API_URL}/user/${id}/favorites/posts`,
      };
    });
  }
}
