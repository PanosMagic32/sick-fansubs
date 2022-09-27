import { Component, OnDestroy, OnInit } from '@angular/core';
import { PageEvent } from '@angular/material/paginator';
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
  totalPosts = 0;
  postsPerPage = 5;
  currentPage = 0;
  pageSizeOptions = [5, 10];
  isLoading = false;

  constructor(private blogPostService: BlogPostService) {}

  ngOnInit(): void {
    this.isLoading = true;
    this.blogPostService.getBlogPosts(this.postsPerPage, this.currentPage);

    this.blogPostSubscription = this.blogPostService.getPostUpdateListener().subscribe({
      next: (res) => {
        // console.log(res);
        this.posts = res.posts;
        this.totalPosts = res.count;
        this.isLoading = false;
      },
    });
  }

  ngOnDestroy(): void {
    if (this.blogPostSubscription) {
      this.blogPostSubscription.unsubscribe();
    }
  }

  onChangedPage(pageData: PageEvent) {
    this.currentPage = pageData.pageIndex;
    this.postsPerPage = pageData.pageSize;

    this.blogPostService.getBlogPosts(this.postsPerPage, this.currentPage);
  }
}
