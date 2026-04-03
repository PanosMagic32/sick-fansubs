import { DatePipe, NgOptimizedImage } from '@angular/common';
import { Component, computed, inject, input, output } from '@angular/core';
import { Router } from '@angular/router';

import { MatMiniFabButton, MatButton, MatIconButton } from '@angular/material/button';
import {
  MatCard,
  MatCardActions,
  MatCardAvatar,
  MatCardContent,
  MatCardHeader,
  MatCardImage,
  MatCardSubtitle,
  MatCardTitle,
} from '@angular/material/card';
import { MatChip } from '@angular/material/chips';
import { MatDivider } from '@angular/material/divider';
import { MatIcon } from '@angular/material/icon';
import { MatMenuModule } from '@angular/material/menu';

import type { BlogPost } from '../../data-access/blog-post.interface';

@Component({
  selector: 'sf-blog-post-item',
  templateUrl: './blog-post-item.component.html',
  styleUrl: './blog-post-item.component.scss',
  imports: [
    NgOptimizedImage,
    DatePipe,
    MatCard,
    MatCardImage,
    MatCardAvatar,
    MatCardHeader,
    MatCardTitle,
    MatCardSubtitle,
    MatCardContent,
    MatCardActions,
    MatIcon,
    MatDivider,
    MatMiniFabButton,
    MatIconButton,
    MatChip,
    MatMenuModule,
    MatButton,
  ],
})
export class BlogPostItemComponent {
  private readonly router = inject(Router);
  private readonly defaultAvatarPath = '/logo/logo.png';

  readonly blogPost = input.required<BlogPost>();
  readonly index = input.required<number>();
  readonly currentPage = input.required<number>();
  readonly isAdmin = input.required<boolean>();
  readonly isAuthenticated = input.required<boolean>();
  readonly isFavorite = input.required<boolean>();
  readonly isFavoriteActionPending = input.required<boolean>();

  readonly favoriteToggle = output<string | undefined>();

  protected readonly imgDownloadPriority = computed(() => this.index() === 0 || this.index() === 1);
  protected readonly creatorAvatarUrl = computed(() => this.blogPost().creator?.avatar || this.defaultAvatarPath);
  protected readonly createdByUsername = computed(() => this.blogPost().creator?.username || 'Kushoyarou');
  protected readonly editedByUsername = computed(
    () => this.blogPost().updatedBy?.username || this.blogPost().creator?.username || 'Kushoyarou',
  );
  protected readonly hasBeenEdited = computed(() => {
    if (!this.blogPost().updatedBy) {
      return false;
    }

    const updatedAt = this.blogPost().updatedAt;
    const createdAt = this.blogPost().dateTimeCreated;

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

  onDownload(url: string | undefined) {
    if (url) {
      const downloadURL = new URL(url);
      const a = document.createElement('a');
      a.href = downloadURL.href;
      a.download = downloadURL.pathname.split('/').pop() || '';
      a.click();
    }
  }

  onEdit() {
    this.router.navigate([this.blogPost()._id, 'edit'], { replaceUrl: true });
  }

  onFavoriteToggle() {
    this.favoriteToggle.emit(this.blogPost()._id);
  }
}
