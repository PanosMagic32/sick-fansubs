import { HttpClient } from '@angular/common/http';
import { Injectable } from '@angular/core';
import { Router } from '@angular/router';
import { BehaviorSubject } from 'rxjs';

import { MatSnackBar } from '@angular/material/snack-bar';

import { ConfigService } from '@sick/shared';

import { BlogPost, BlogPostResponse } from './blog-post.interface';

@Injectable({
  providedIn: 'root',
})
export class BlogPostService {
  private isLoading = new BehaviorSubject(false);
  private posts = new BehaviorSubject<BlogPostResponse>({ posts: [], count: 0 });

  get isLoading$() {
    return this.isLoading.asObservable();
  }

  get posts$() {
    return this.posts.asObservable();
  }

  constructor(
    private readonly http: HttpClient,
    private readonly snackBar: MatSnackBar,
    private readonly configService: ConfigService,
    private readonly router: Router,
  ) {}

  createBlogPost(post: BlogPost) {
    this.http.post(`${this.configService.API_URL}/blog-post`, post).subscribe({
      next: () => {
        this.isLoading.next(false);
        this.router.navigate(['/'], { replaceUrl: true });
      },
      error: (err) => {
        this.openSnackBar(err.status === 0 ? 'Uknown error.' : err.error.message, 'OK');
        this.isLoading.next(false);
      },
    });
  }

  getBlogPosts(postsPerPage: number, currentPage: number) {
    this.isLoading.next(true);

    this.http
      .get<BlogPostResponse>(`${this.configService.API_URL}/blog-post?pagesize=${postsPerPage}&page=${currentPage}`)
      .subscribe({
        next: (res) => {
          this.posts.next({
            posts: [...res.posts],
            count: res.count,
          });

          this.isLoading.next(false);
        },
        error: (err) => {
          this.openSnackBar(err.status === 0 ? 'Uknown error.' : err.error.message, 'OK');
          this.isLoading.next(false);
        },
      });
  }

  getBlogPostById(id: string) {
    return this.http.get<BlogPost>(`${this.configService.API_URL}/blog-post/${id}`);
  }

  updateBlogPost(id: string, updateBlogPost: BlogPost) {
    this.isLoading.next(true);

    this.http.patch<BlogPost>(`${this.configService.API_URL}/blog-post/${id}`, updateBlogPost).subscribe({
      next: () => {
        this.isLoading.next(false);
        this.router.navigate(['/'], { replaceUrl: true });
      },
      error: (err) => {
        this.openSnackBar(err.status === 0 ? 'Uknown error.' : err.error.message, 'OK');
        this.isLoading.next(false);
      },
    });
  }

  deleteBlogPost(id: string) {
    this.http.delete(`${this.configService.API_URL}/blog-post/${id}`).subscribe({
      next: () => {
        this.isLoading.next(false);
        this.router.navigate(['/'], { replaceUrl: true });
      },
      error: (err) => {
        this.openSnackBar(err.status === 0 ? 'Uknown error.' : err.error.message, 'OK');
        this.isLoading.next(false);
      },
    });
  }

  private openSnackBar(message: string, action: string) {
    this.snackBar.open(message, action, { duration: 3000 });
  }
}
