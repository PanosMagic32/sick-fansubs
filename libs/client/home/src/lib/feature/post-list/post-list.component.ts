import { Component, OnInit } from '@angular/core';
import { PageEvent } from '@angular/material/paginator';

import { BlogPost } from '@sick/api/blog-post';

import { BlogPostService } from '../../data-access/blog-post.service';

@Component({
  selector: 'sick-post-list',
  templateUrl: './post-list.component.html',
  styles: [],
})
export class PostListComponent implements OnInit {
  posts: BlogPost[] = [];
  totalCount = -1;
  isLoading = false;

  constructor(private blogPostService: BlogPostService) {}

  ngOnInit(): void {
    this.fetchBlogPosts();
  }

  onPageEvent(event: PageEvent) {
    this.fetchBlogPosts(event.pageIndex > 0 ? event.pageIndex * 5 : 0, event.pageSize > 5 ? event.pageSize : 5);
  }

  private fetchBlogPosts(skip = 0, limit = 5) {
    this.isLoading = true;
    this.blogPostService.getBlogPosts(skip, limit).subscribe({
      next: (res) => {
        console.log(res);
        this.posts = res.posts;
        this.totalCount = res.count;
        this.isLoading = false;
      },
      error: (err) => {
        this.isLoading = false;
        console.log(err);
      },
    });
  }
}
