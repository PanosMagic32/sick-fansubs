import { Component, OnInit } from '@angular/core';
import { PageEvent } from '@angular/material/paginator';
import { tap } from 'rxjs';

import { TokenService } from '@sick/client/auth';

import { BlogPostService } from '../../data-access/blog-post.service';

@Component({
  selector: 'sick-post-list',
  templateUrl: './post-list.component.html',
  styleUrls: ['./post-list.component.scss'],
})
export class PostListComponent implements OnInit {
  totalPosts = 0;
  postsPerPage = 5;
  currentPage = 0;
  pageSizeOptions = [5, 10, 20];
  isAdmin$ = this.tokenService.isAdmin$;

  isLoading$ = this.blogPostService.isLoading$;
  posts$ = this.blogPostService.posts$.pipe(tap((response) => (this.totalPosts = response.count)));

  constructor(
    private readonly blogPostService: BlogPostService,
    private readonly tokenService: TokenService,
  ) {}

  ngOnInit(): void {
    this.blogPostService.getBlogPosts(this.postsPerPage, this.currentPage);
  }

  onChangedPage(pageData: PageEvent) {
    this.currentPage = pageData.pageIndex;
    this.postsPerPage = pageData.pageSize;

    this.blogPostService.getBlogPosts(this.postsPerPage, this.currentPage);
  }
}
