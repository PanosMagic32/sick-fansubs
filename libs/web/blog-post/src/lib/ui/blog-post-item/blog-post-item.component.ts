import { DatePipe, NgOptimizedImage } from '@angular/common';
import { Component, computed, inject, input } from '@angular/core';
import { Router } from '@angular/router';

import { MatMiniFabButton, MatButton } from '@angular/material/button';
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
    MatChip,
    MatMenuModule,
    MatButton,
  ],
})
export class BlogPostItemComponent {
  private readonly router = inject(Router);

  readonly blogPost = input.required<BlogPost>();
  readonly index = input.required<number>();
  readonly currentPage = input.required<number>();
  readonly isAdmin = input.required<boolean>();

  protected readonly imgDownloadPriority = computed(() => this.index() === 0 || this.index() === 1);

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
}
