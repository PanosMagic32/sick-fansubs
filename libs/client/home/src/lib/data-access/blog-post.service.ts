import { HttpClient } from '@angular/common/http';
import { Injectable } from '@angular/core';
import { BehaviorSubject, Subject } from 'rxjs';

import { MatSnackBar } from '@angular/material/snack-bar';

import { BlogPost } from '@sick/api/blog-post';
import { ConfigService } from '@sick/shared';

@Injectable({
  providedIn: 'root',
})
export class BlogPostService {
  isLoading$ = new BehaviorSubject(false);

  private posts: BlogPost[] = [];
  private postsUpdated = new Subject<{ posts: BlogPost[]; count: number }>();

  constructor(private http: HttpClient, private snackBar: MatSnackBar, private configService: ConfigService) {}

  getBlogPosts(postsPerPage: number, currentPage: number) {
    this.isLoading$.next(true);

    this.http
      .get<{ posts: BlogPost[]; count: number }>(
        `${this.configService.API_URL}/blog-post?pagesize=${postsPerPage}&page=${currentPage}`
      )
      .subscribe({
        next: (res) => {
          this.posts = res.posts;
          this.postsUpdated.next({
            posts: [...this.posts],
            count: res.count,
          });

          this.isLoading$.next(false);
        },
        error: (err) => {
          this.openSnackBar(err.status === 0 ? 'Uknown error.' : err.error.message, 'OK');
          this.isLoading$.next(false);
        },
      });
  }

  getPostUpdateListener() {
    return this.postsUpdated.asObservable();
  }

  private openSnackBar(message: string, action: string) {
    this.snackBar.open(message, action, { duration: 3000 });
  }
}
