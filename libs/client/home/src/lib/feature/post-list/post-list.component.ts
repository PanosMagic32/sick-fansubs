import { Component, OnDestroy, OnInit } from '@angular/core';
import { PageEvent } from '@angular/material/paginator';
import { MatSnackBar } from '@angular/material/snack-bar';
import { Subscription } from 'rxjs';

import { BlogPost } from '@sick/api/blog-post';

import { BlogPostService } from '../../data-access/blog-post.service';

@Component({
  selector: 'sick-post-list',
  templateUrl: './post-list.component.html',
  styles: [],
})
export class PostListComponent implements OnInit, OnDestroy {
  blogPostSubscription!: Subscription;
  posts: BlogPost[] = [];
  totalCount = -1;
  isLoading = false;

  constructor(private blogPostService: BlogPostService, private snackBar: MatSnackBar) {}

  ngOnInit(): void {
    this.fetchBlogPosts();
  }

  ngOnDestroy(): void {
    if (this.blogPostSubscription) {
      this.blogPostSubscription.unsubscribe();
    }
  }

  onPageEvent(event: PageEvent) {
    this.fetchBlogPosts(event.pageIndex > 0 ? event.pageIndex * 5 : 0, event.pageSize > 5 ? event.pageSize : 5);
  }

  private fetchBlogPosts(skip = 0, limit = 5) {
    this.isLoading = true;
    this.blogPostSubscription = this.blogPostService.getBlogPosts(skip, limit).subscribe({
      next: (res) => {
        console.log(res);
        this.posts = res.posts;
        this.totalCount = res.count;
        this.isLoading = false;
      },
      error: (err) => {
        this.isLoading = false;

        this.openSnackBar(err.status === 0 ? 'Uknown error.' : err.error.message, 'OK');
      },
    });
  }

  private openSnackBar(message: string, action: string) {
    this.snackBar.open(message, action, { duration: 3000 });
  }
}
