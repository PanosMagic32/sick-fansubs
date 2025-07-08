import { HttpClient } from '@angular/common/http';
import { inject, Injectable, signal } from '@angular/core';
import { Router } from '@angular/router';

import { MatSnackBar } from '@angular/material/snack-bar';

import { WebConfigService } from '@web/shared';

import type { BlogPost, BlogPostResponse, EditBlogPost } from './blog-post.interface';

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

  createBlogPost(post: BlogPost) {
    this.httpClient.post(`${this.webConfigService.API_URL}/blog-post`, post).subscribe({
      next: () => {
        this._isLoading.set(false);
        this.router.navigate(['/'], { replaceUrl: true });
      },
      error: (err) => {
        this.openSnackBar(err.status === 0 ? 'Uknown error.' : err.error.message, 'OK');
        this._isLoading.set(false);
      },
    });
  }

  getBlogPosts(postsPerPage: number, currentPage: number) {
    this._isLoading.set(true);

    this.httpClient
      .get<BlogPostResponse>(`${this.webConfigService.API_URL}/blog-post?pagesize=${postsPerPage}&page=${currentPage}`)
      .subscribe({
        next: (res) => {
          this._posts.set({
            posts: [...res.posts],
            count: res.count,
          });

          this._isLoading.set(false);
        },
        error: (err) => {
          this.openSnackBar(err.status === 0 ? 'Uknown error.' : err.error.message, 'OK');
          this._isLoading.set(false);
        },
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
        this.openSnackBar(err.status === 0 ? 'Uknown error.' : err.error.message, 'OK');
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
        this.openSnackBar(err.status === 0 ? 'Uknown error.' : err.error.message, 'OK');
        this._isLoading.set(false);
      },
    });
  }

  private openSnackBar(message: string, action: string) {
    this.snackBar.open(message, action, { duration: 3000 });
  }
}
