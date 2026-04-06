import { DatePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, input, output } from '@angular/core';

import { MatButton, MatIconButton } from '@angular/material/button';
import { MatCard, MatCardContent, MatCardHeader, MatCardTitle } from '@angular/material/card';
import { MatDivider } from '@angular/material/divider';
import { MatIcon } from '@angular/material/icon';
import { MatPaginator, type PageEvent } from '@angular/material/paginator';
import { MatProgressSpinner } from '@angular/material/progress-spinner';
import { MatTooltipModule } from '@angular/material/tooltip';

import type { Project } from '@shared/types';

import { FavoriteProjectsViewState, FavoriteSortOrder, FavoritesPageChange } from '../../data-access/types';

@Component({
  selector: 'sf-account-favorite-projects',
  standalone: true,
  templateUrl: './account-favorite-projects.component.html',
  styleUrl: './account-favorite-projects.component.scss',
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
    MatPaginator,
    MatTooltipModule,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AccountFavoriteProjectsComponent {
  readonly favoriteProjectIds = input.required<string[]>();
  readonly favoriteProjects = input.required<Project[]>();
  readonly favoriteProjectsTotal = input.required<number>();
  readonly currentPage = input.required<number>();
  readonly pageSize = input.required<number>();
  readonly pageSizeOptions = input.required<number[]>();
  readonly isFavoriteIdsLoading = input.required<boolean>();
  readonly hasFavoriteIdsError = input.required<boolean>();
  readonly isFavoriteProjectsLoading = input.required<boolean>();
  readonly hasFavoriteProjectsError = input.required<boolean>();
  readonly isRemovingFavorite = input.required<boolean>();
  readonly sortOrder = input<FavoriteSortOrder>('newest');

  readonly retryLoadFavoriteIds = output<void>();
  readonly retryLoadFavoriteProjects = output<void>();
  readonly removeFavorite = output<string>();
  readonly openProject = output<string>();
  readonly pageChange = output<FavoritesPageChange>();
  readonly toggleSort = output<void>();

  readonly viewState = computed<FavoriteProjectsViewState>(() => {
    if (this.isFavoriteIdsLoading()) return 'loading-ids';
    if (this.hasFavoriteIdsError()) return 'ids-error';
    if (!this.favoriteProjectIds().length) return 'empty-ids';
    if (this.isFavoriteProjectsLoading()) return 'loading-projects';
    if (this.hasFavoriteProjectsError()) return 'projects-error';
    if (!this.favoriteProjects().length) return 'empty-projects';
    return 'ready';
  });

  onRetryLoadFavoriteIds(): void {
    this.retryLoadFavoriteIds.emit();
  }

  onRetryLoadFavoriteProjects(): void {
    this.retryLoadFavoriteProjects.emit();
  }

  onRemoveFavorite(projectId: string | undefined): void {
    if (!projectId || this.isRemovingFavorite()) {
      return;
    }

    this.removeFavorite.emit(projectId);
  }

  onOpenProject(projectId: string | undefined): void {
    if (!projectId) {
      return;
    }

    this.openProject.emit(projectId);
  }

  onPageChange(event: PageEvent): void {
    this.pageChange.emit({
      page: event.pageIndex + 1,
      pageSize: event.pageSize,
    });
  }

  onToggleSort(): void {
    this.toggleSort.emit();
  }
}
