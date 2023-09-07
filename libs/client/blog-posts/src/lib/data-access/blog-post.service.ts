import { HttpClient } from '@angular/common/http';
import { Injectable } from '@angular/core';
import { BehaviorSubject } from 'rxjs';

import { MatSnackBar } from '@angular/material/snack-bar';

import { ConfigService } from '@sick/shared';

import { BlogPostResponse } from './blog-post.interface';

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
    private http: HttpClient,
    private snackBar: MatSnackBar,
    private configService: ConfigService,
  ) {}

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

  private openSnackBar(message: string, action: string) {
    this.snackBar.open(message, action, { duration: 3000 });
  }
}
