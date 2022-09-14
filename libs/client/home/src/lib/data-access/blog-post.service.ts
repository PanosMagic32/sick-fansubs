import { HttpClient } from '@angular/common/http';
import { Injectable } from '@angular/core';
import { Subject } from 'rxjs';

import { MatSnackBar } from '@angular/material/snack-bar';

import { BlogPost } from '@sick/api/blog-post';

@Injectable({
  providedIn: 'root',
})
export class BlogPostService {
  private posts: BlogPost[] = [];
  private postsUpdated = new Subject<{ posts: BlogPost[]; count: number }>();

  constructor(private http: HttpClient, private snackBar: MatSnackBar) {}

  getBlogPosts(postsPerPage: number, currentPage: number) {
    this.http
      .get<{ posts: BlogPost[]; count: number }>(
        `http://localhost:3333/api/blog-post?pagesize=${postsPerPage}&page=${currentPage}`
      )
      .subscribe({
        next: (res) => {
          this.posts = res.posts;
          this.postsUpdated.next({
            posts: [...this.posts],
            count: res.count,
          });
        },
        error: (err) => {
          this.openSnackBar(err.status === 0 ? 'Uknown error.' : err.error.message, 'OK');
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
