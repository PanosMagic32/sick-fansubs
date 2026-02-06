import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { ActivatedRoute, Router } from '@angular/router';
import { map, tap } from 'rxjs';

import { MatFabButton } from '@angular/material/button';
import { MatIcon } from '@angular/material/icon';
import { MatPaginator, type PageEvent } from '@angular/material/paginator';
import { MatProgressBar } from '@angular/material/progress-bar';

import { NoContentComponent, TokenService } from '@web/shared';

import { BlogPostService } from '../../data-access/blog-post.service';
import { BlogPostItemComponent } from '../../ui/blog-post-item/blog-post-item.component';

const DEFAULT_PAGE = 1;
const DEFAULT_PAGE_SIZE = 10;
const PAGE_SIZE_OPTIONS = [10, 20, 50, 100];

@Component({
  selector: 'sf-blog-post-list',
  templateUrl: './blog-post-list.component.html',
  styleUrl: './blog-post-list.component.scss',
  imports: [NoContentComponent, MatProgressBar, MatIcon, MatPaginator, MatFabButton, BlogPostItemComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class BlogPostListComponent {
  private readonly blogPostService = inject(BlogPostService);
  private readonly tokenService = inject(TokenService);
  private readonly router = inject(Router);
  private readonly activatedRoute = inject(ActivatedRoute);

  protected readonly currentPage = signal(DEFAULT_PAGE);
  protected readonly pageSize = signal(DEFAULT_PAGE_SIZE);
  protected readonly pageSizeOptions = signal(PAGE_SIZE_OPTIONS);

  protected readonly isAdmin = this.tokenService.isAdmin;

  protected readonly blogPosts = this.blogPostService.getBlogPosts(this.pageSize, this.currentPage);

  constructor() {
    this.activatedRoute.queryParamMap
      .pipe(
        takeUntilDestroyed(),
        map((params) => ({
          page: +(params.get('page') ?? this.currentPage()),
          pageSize: +(params.get('pageSize') ?? this.pageSize()),
        })),
        tap(({ page, pageSize }) => {
          this.currentPage.set(page);
          this.pageSize.set(pageSize);
        }),
      )
      .subscribe();
  }

  onCreatePost() {
    this.router.navigate(['create']);
  }

  onPageChange(event: PageEvent) {
    this.handleQueryParamsNavigation({ page: event.pageIndex + 1, pageSize: event.pageSize });
  }

  private handleQueryParamsNavigation(queryParams: Partial<{ page: number; pageSize: number }>) {
    this.router.navigate([], {
      queryParams,
      queryParamsHandling: 'merge',
      relativeTo: this.activatedRoute,
    });
  }
}
