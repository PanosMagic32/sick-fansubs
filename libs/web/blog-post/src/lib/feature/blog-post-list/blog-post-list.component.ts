import { ChangeDetectionStrategy, Component, computed, inject, type OnInit } from '@angular/core';
import { Router } from '@angular/router';

import { MatFabButton } from '@angular/material/button';
import { MatIcon } from '@angular/material/icon';
import { MatPaginator, type PageEvent } from '@angular/material/paginator';
import { MatProgressBar } from '@angular/material/progress-bar';

import { NoContentComponent, TokenService } from '@web/shared';

import { BlogPostService } from '../../data-access/blog-post.service';
import { BlogPostItemComponent } from '../../ui/blog-post-item/blog-post-item.component';

@Component({
  selector: 'sf-blog-post-list',
  templateUrl: './blog-post-list.component.html',
  standalone: true,
  imports: [NoContentComponent, MatProgressBar, MatIcon, MatPaginator, MatFabButton, BlogPostItemComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class BlogPostListComponent implements OnInit {
  private readonly blogPostService = inject(BlogPostService);
  private readonly tokenService = inject(TokenService);
  private readonly router = inject(Router);

  protected postsPerPage = 5;
  protected currentPage = 0;
  protected pageSizeOptions = [5, 10, 20, 50, 100];

  protected readonly isAdmin = this.tokenService.isAdmin;
  protected readonly isLoading = this.blogPostService.isLoading;
  protected readonly posts = this.blogPostService.posts;

  protected readonly totalPosts = computed(() => this.posts().count);

  ngOnInit() {
    this.blogPostService.getBlogPosts(this.postsPerPage, this.currentPage);
  }

  onChangedPage(pageData: PageEvent) {
    this.currentPage = pageData.pageIndex;
    this.postsPerPage = pageData.pageSize;

    this.blogPostService.getBlogPosts(this.postsPerPage, this.currentPage);
  }

  onCreatePost() {
    this.router.navigate(['create']);
  }
}
