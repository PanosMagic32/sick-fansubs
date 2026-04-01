import { HttpClient, httpResource, HttpResourceRef } from '@angular/common/http';
import { inject, Injectable, Signal, WritableSignal } from '@angular/core';
import { Observable } from 'rxjs';

import { WebConfigService } from '@web/shared';
import {
  UserProfile,
  UpdateUserRequest,
  FavoriteBlogPostIdsResponse,
  FavoriteBlogPostsResponse,
  MediaUploadResponse,
} from './types';

@Injectable({ providedIn: 'root' })
export class UserService {
  private readonly webConfigService = inject(WebConfigService);
  private readonly httpClient = inject(HttpClient);

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

  getFavoriteBlogPosts(
    userId: Signal<string>,
    pageSize: Signal<number>,
    currentPage: Signal<number>,
  ): HttpResourceRef<FavoriteBlogPostsResponse | undefined> {
    return httpResource<FavoriteBlogPostsResponse>(() => {
      const id = userId();
      if (!id) return;

      return {
        url: `${this.webConfigService.API_URL}/user/${id}/favorites/posts?pagesize=${pageSize()}&page=${currentPage()}`,
      };
    });
  }

  uploadAvatarImage(file: File): Observable<MediaUploadResponse> {
    const formData = new FormData();
    formData.append('file', file);

    return this.httpClient.post<MediaUploadResponse>(`${this.webConfigService.API_URL}/media/images`, formData);
  }
}
