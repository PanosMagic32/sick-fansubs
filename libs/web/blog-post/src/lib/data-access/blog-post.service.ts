import { HttpClient, httpResource, HttpResourceRef } from '@angular/common/http';
import { inject, Injectable, Signal, signal, WritableSignal } from '@angular/core';
import { Router } from '@angular/router';

import { MatSnackBar } from '@angular/material/snack-bar';

import { WebConfigService } from '@web/shared';

import type { BlogPost, BlogPostResponse, CreateBlogPost, EditBlogPost } from './blog-post.interface';

@Injectable({ providedIn: 'root' })
export class BlogPostService {
  private readonly httpClient = inject(HttpClient);
  private readonly snackBar = inject(MatSnackBar);
  private readonly webConfigService = inject(WebConfigService);
  private readonly router = inject(Router);

  private _isLoading = signal(false);
  isLoading = this._isLoading.asReadonly();

  private _posts = signal<BlogPostResponse>({ posts: [], count: 0 });
  posts = this._posts.asReadonly();

  getBlogPosts(postsPerPage: Signal<number>, currentPage: Signal<number>): HttpResourceRef<BlogPostResponse | undefined> {
    return httpResource<BlogPostResponse>(() => ({
      url: `${this.webConfigService.API_URL}/blog-post?pagesize=${postsPerPage()}&page=${currentPage() - 1}`,
    }));
  }

  createBlogPost(post: WritableSignal<CreateBlogPost | null>) {
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

  getBlogPostById(id: string) {
    return this.httpClient.get<BlogPost>(`${this.webConfigService.API_URL}/blog-post/${id}`);
  }

  updateBlogPost(id: string, updateBlogPost: EditBlogPost) {
    this._isLoading.set(true);

    this.httpClient.patch<BlogPost>(`${this.webConfigService.API_URL}/blog-post/${id}`, updateBlogPost).subscribe({
      next: () => {
        this._isLoading.set(false);
        this.router.navigate(['/'], { replaceUrl: true });
      },
      error: (err) => {
        this.openSnackBar(err.status === 0 ? 'Άγνωστο σφάλμα.' : err.error.message, 'OK');
        this._isLoading.set(false);
      },
    });
  }

  deleteBlogPost(id: string) {
    this.httpClient.delete(`${this.webConfigService.API_URL}/blog-post/${id}`).subscribe({
      next: () => {
        this._isLoading.set(false);
        this.router.navigate(['/'], { replaceUrl: true });
      },
      error: (err) => {
        this.openSnackBar(err.status === 0 ? 'Άγνωστο σφάλμα.' : err.error.message, 'OK');
        this._isLoading.set(false);
      },
    });
  }

  private openSnackBar(message: string, action: string) {
    this.snackBar.open(message, action, { duration: 3000 });
  }
}
