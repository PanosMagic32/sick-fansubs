import { HttpClient } from '@angular/common/http';
import { Injectable } from '@angular/core';
import { Observable } from 'rxjs';

import { BlogPost } from '@sick/api/blog-post';

@Injectable({
  providedIn: 'root',
})
export class BlogPostService {
  constructor(private http: HttpClient) {}

  getBlogPosts(skip: number, limit: number): Observable<{ posts: BlogPost[]; count: number }> {
    return this.http.get<{ posts: BlogPost[]; count: number }>(
      `http://localhost:3333/api/blog-post?skip=${skip}&limit=${limit}`
    );
  }
}
