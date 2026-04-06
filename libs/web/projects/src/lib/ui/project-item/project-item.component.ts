import { DatePipe, NgOptimizedImage } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, input, output } from '@angular/core';
import { ActivatedRoute, Router } from '@angular/router';

import { MatButton, MatIconButton, MatMiniFabButton } from '@angular/material/button';
import {
  MatCard,
  MatCardActions,
  MatCardAvatar,
  MatCardContent,
  MatCardHeader,
  MatCardImage,
  MatCardTitle,
} from '@angular/material/card';
import { MatDivider } from '@angular/material/divider';
import { MatIcon } from '@angular/material/icon';

import type { Project } from '../../data-access/project.interface';

@Component({
  selector: 'sf-project-item',
  templateUrl: './project-item.component.html',
  styleUrl: './project-item.component.scss',
  imports: [
    NgOptimizedImage,
    DatePipe,
    MatCard,
    MatCardImage,
    MatCardAvatar,
    MatCardHeader,
    MatCardTitle,
    MatCardContent,
    MatCardActions,
    MatButton,
    MatIconButton,
    MatIcon,
    MatDivider,
    MatMiniFabButton,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ProjectItemComponent {
  private readonly router = inject(Router);
  private readonly activatedRoute = inject(ActivatedRoute);
  private readonly defaultAvatarPath = '/logo/logo.png';

  readonly project = input.required<Project>();
  readonly index = input.required<number>();
  readonly canManageContent = input.required<boolean>();
  readonly isAuthenticated = input.required<boolean>();
  readonly isFavorite = input.required<boolean>();
  readonly isFavoriteActionPending = input.required<boolean>();

  readonly favoriteToggle = output<string | undefined>();

  protected readonly imgDownloadPriority = computed(() => this.index() === 0 || this.index() === 1);
  protected readonly creatorAvatarUrl = computed(() => this.project().creator?.avatar || this.defaultAvatarPath);
  protected readonly createdByUsername = computed(() => this.project().creator?.username || 'Kushoyarou');
  protected readonly editedByUsername = computed(
    () => this.project().updatedBy?.username || this.project().creator?.username || 'Kushoyarou',
  );
  protected readonly hasBeenEdited = computed(() => {
    if (!this.project().updatedBy) {
      return false;
    }

    const updatedAt = this.project().updatedAt;
    const createdAt = this.project().dateTimeCreated;

    if (!updatedAt || !createdAt) {
      return false;
    }

    const updatedMs = new Date(updatedAt).getTime();
    const createdMs = new Date(createdAt).getTime();

    if (Number.isNaN(updatedMs) || Number.isNaN(createdMs)) {
      return false;
    }

    return updatedMs > createdMs;
  });

  onEdit() {
    this.router.navigate([this.project()._id, 'edit'], {
      relativeTo: this.activatedRoute,
    });
  }

  onMore() {
    this.router.navigate([this.project()._id], {
      relativeTo: this.activatedRoute,
    });
  }

  onFavoriteToggle() {
    this.favoriteToggle.emit(this.project()._id);
  }
}
