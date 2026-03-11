import { ChangeDetectionStrategy, Component, computed, effect, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { ActivatedRoute, Router } from '@angular/router';
import { map, tap } from 'rxjs';

import { MatFabButton } from '@angular/material/button';
import { MatIcon } from '@angular/material/icon';
import { MatPaginator, type PageEvent } from '@angular/material/paginator';
import { MatProgressBar } from '@angular/material/progress-bar';
import { MatSnackBar } from '@angular/material/snack-bar';

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
  private readonly snackBar = inject(MatSnackBar);

  private readonly addFavoriteRequest = signal<string | null>(null);
  private readonly removeFavoriteRequest = signal<string | null>(null);
  private readonly favoriteBlogPostIdsState = signal<string[]>([]);

  protected readonly currentPage = signal(DEFAULT_PAGE);
  protected readonly pageSize = signal(DEFAULT_PAGE_SIZE);
  protected readonly pageSizeOptions = signal(PAGE_SIZE_OPTIONS);

  protected readonly isAdmin = this.tokenService.isAdmin;
  protected readonly userId = this.tokenService.userId;
  protected readonly activeUserId = computed(() => (this.tokenService.isValidToken() ? this.userId() : null));
  protected readonly favoriteBlogPostIds = this.favoriteBlogPostIdsState.asReadonly();

  protected readonly isAuthenticated = computed(() => Boolean(this.activeUserId()));
  protected readonly favoriteActionPendingPostId = computed(() => this.addFavoriteRequest() ?? this.removeFavoriteRequest());

  protected readonly blogPosts = this.blogPostService.getBlogPosts(this.pageSize, this.currentPage);
  protected readonly favoriteBlogPostIdsResource = this.blogPostService.getFavoriteBlogPostIds(this.activeUserId);
  protected readonly addFavoriteResource = this.blogPostService.addFavoriteBlogPost(
    this.activeUserId,
    this.addFavoriteRequest,
  );
  protected readonly removeFavoriteResource = this.blogPostService.removeFavoriteBlogPost(
    this.activeUserId,
    this.removeFavoriteRequest,
  );

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

    effect(() => {
      if (!this.activeUserId()) {
        this.favoriteBlogPostIdsState.set([]);
      }
    });

    effect(() => {
      if (!this.favoriteBlogPostIdsResource.hasValue()) {
        return;
      }

      const favoriteIds = this.favoriteBlogPostIdsResource.value()?.favoriteBlogPostIds;
      if (favoriteIds) {
        this.favoriteBlogPostIdsState.set([...favoriteIds]);
      }
    });

    effect(() => {
      const error = this.addFavoriteResource.error();
      if (error) {
        this.snackBar.open('Αποτυχία προσθήκης στα αγαπημένα.', 'OK', { duration: 3000 });
        this.addFavoriteRequest.set(null);
        return;
      }

      if (!this.addFavoriteResource.hasValue()) {
        return;
      }

      const favoriteIds = this.addFavoriteResource.value()?.favoriteBlogPostIds;
      if (favoriteIds) {
        this.favoriteBlogPostIdsState.set([...favoriteIds]);
        this.snackBar.open('Η ανάρτηση προστέθηκε στα αγαπημένα.', 'OK', { duration: 3000 });
        this.addFavoriteRequest.set(null);
      }
    });

    effect(() => {
      const error = this.removeFavoriteResource.error();
      if (error) {
        this.snackBar.open('Αποτυχία αφαίρεσης από τα αγαπημένα.', 'OK', { duration: 3000 });
        this.removeFavoriteRequest.set(null);
        return;
      }

      if (!this.removeFavoriteResource.hasValue()) {
        return;
      }

      const favoriteIds = this.removeFavoriteResource.value()?.favoriteBlogPostIds;
      if (favoriteIds) {
        this.favoriteBlogPostIdsState.set([...favoriteIds]);
        this.snackBar.open('Η ανάρτηση αφαιρέθηκε από τα αγαπημένα.', 'OK', { duration: 3000 });
        this.removeFavoriteRequest.set(null);
      }
    });
  }

  onCreatePost() {
    this.router.navigate(['create']);
  }

  onToggleFavorite(postId: string | undefined) {
    if (!postId) return;

    if (!this.activeUserId()) {
      this.router.navigate(['/auth/login']);
      return;
    }

    if (this.favoriteActionPendingPostId()) return;

    if (this.isFavorite(postId)) {
      this.removeFavoriteRequest.set(postId);
      return;
    }

    this.addFavoriteRequest.set(postId);
  }

  protected isFavorite(postId: string | undefined): boolean {
    if (!postId) {
      return false;
    }

    return this.favoriteBlogPostIds().includes(postId);
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
