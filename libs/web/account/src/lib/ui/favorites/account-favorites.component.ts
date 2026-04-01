import { DatePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, input, output } from '@angular/core';

import { MatButton, MatIconButton } from '@angular/material/button';
import { MatCard, MatCardContent, MatCardHeader, MatCardTitle } from '@angular/material/card';
import { MatDivider } from '@angular/material/divider';
import { MatIcon } from '@angular/material/icon';
import { MatMenuModule } from '@angular/material/menu';
import { MatPaginator, type PageEvent } from '@angular/material/paginator';
import { MatProgressSpinner } from '@angular/material/progress-spinner';

import type { BlogPost } from '@shared/types';

import { FavoritesPageChange, FavoritesViewState } from '../../data-access/types';

@Component({
  selector: 'sf-account-favorites',
  templateUrl: './account-favorites.component.html',
  styleUrl: './account-favorites.component.scss',
  imports: [
    DatePipe,
    MatCard,
    MatCardHeader,
    MatCardTitle,
    MatCardContent,
    MatDivider,
    MatIcon,
    MatProgressSpinner,
    MatButton,
    MatIconButton,
    MatMenuModule,
    MatPaginator,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AccountFavoritesComponent {
  readonly favoriteBlogPostIds = input.required<string[]>();
  readonly favoriteBlogPosts = input.required<BlogPost[]>();
  readonly favoritePostsTotal = input.required<number>();
  readonly currentPage = input.required<number>();
  readonly pageSize = input.required<number>();
  readonly pageSizeOptions = input.required<number[]>();
  readonly isFavoriteIdsLoading = input.required<boolean>();
  readonly hasFavoriteIdsError = input.required<boolean>();
  readonly isFavoritePostsLoading = input.required<boolean>();
  readonly hasFavoritePostsError = input.required<boolean>();
  readonly isRemovingFavorite = input.required<boolean>();

  readonly retryLoadFavoriteIds = output<void>();
  readonly retryLoadFavoritePosts = output<void>();
  readonly removeFavorite = output<string>();
  readonly download = output<string | undefined>();
  readonly pageChange = output<FavoritesPageChange>();

  readonly viewState = computed<FavoritesViewState>(() => {
    if (this.isFavoriteIdsLoading()) return 'loading-ids';
    if (this.hasFavoriteIdsError()) return 'ids-error';
    if (!this.favoriteBlogPostIds().length) return 'empty-ids';
    if (this.isFavoritePostsLoading()) return 'loading-posts';
    if (this.hasFavoritePostsError()) return 'posts-error';
    if (!this.favoriteBlogPosts().length) return 'empty-posts';
    return 'ready';
  });

  onRetryLoadFavoriteIds(): void {
    this.retryLoadFavoriteIds.emit();
  }

  onRetryLoadFavoritePosts(): void {
    this.retryLoadFavoritePosts.emit();
  }

  onRemoveFavorite(postId: string | undefined): void {
    if (!postId || this.isRemovingFavorite()) {
      return;
    }

    this.removeFavorite.emit(postId);
  }

  onDownload(url: string | undefined): void {
    this.download.emit(url);
  }

  onPageChange(event: PageEvent): void {
    this.pageChange.emit({
      page: event.pageIndex + 1,
      pageSize: event.pageSize,
    });
  }
}
