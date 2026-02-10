import { httpResource, HttpResourceRef } from '@angular/common/http';
import { inject, Injectable, Signal, WritableSignal } from '@angular/core';

import { WebConfigService } from '@web/shared';

import type { BlogPost, BlogPostResponse, CreateBlogPost, EditBlogPost } from './blog-post.interface';

@Injectable({ providedIn: 'root' })
export class BlogPostService {
  private readonly webConfigService = inject(WebConfigService);

  getBlogPosts(postsPerPage: Signal<number>, currentPage: Signal<number>): HttpResourceRef<BlogPostResponse | undefined> {
    return httpResource<BlogPostResponse>(() => ({
      url: `${this.webConfigService.API_URL}/blog-post?pagesize=${postsPerPage()}&page=${currentPage() - 1}`,
    }));
  }

  createBlogPost(post: WritableSignal<CreateBlogPost | null>): HttpResourceRef<BlogPost | undefined> {
    return httpResource<BlogPost>(() => {
      const body = post();
      if (!body) return;

      return {
        url: `${this.webConfigService.API_URL}/blog-post`,
        method: 'POST',
        body,
      };
    });
  }

  getBlogPostById(id: Signal<string>): HttpResourceRef<BlogPost | undefined> {
    return httpResource<BlogPost>(() => ({
      url: `${this.webConfigService.API_URL}/blog-post/${id()}`,
    }));
  }

  updateBlogPost(
    id: Signal<string>,
    updateBlogPost: WritableSignal<EditBlogPost | null>,
  ): HttpResourceRef<BlogPost | undefined> {
    return httpResource<BlogPost>(() => {
      const body = updateBlogPost();
      if (!body) return;

      return {
        url: `${this.webConfigService.API_URL}/blog-post/${id()}`,
        method: 'PATCH',
        body,
      };
    });
  }

  deleteBlogPost(id: Signal<string>): HttpResourceRef<void | undefined> {
    return httpResource<void>(() => ({
      url: `${this.webConfigService.API_URL}/blog-post/${id()}`,
      method: 'DELETE',
    }));
  }
}
