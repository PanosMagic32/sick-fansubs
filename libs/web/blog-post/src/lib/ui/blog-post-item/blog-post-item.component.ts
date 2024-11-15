import { DatePipe, NgOptimizedImage } from '@angular/common';
import { Component, computed, inject, input } from '@angular/core';
import { Router } from '@angular/router';

import { MatButton, MatMiniFabButton } from '@angular/material/button';
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
import { MatDivider } from '@angular/material/divider';
import { MatIcon } from '@angular/material/icon';

import type { BlogPost } from '../../data-access/blog-post.interface';

@Component({
  selector: 'sf-blog-post-item',
  templateUrl: './blog-post-item.component.html',
  styleUrl: './blog-post-item.component.scss',
  standalone: true,
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
    MatButton,
    MatIcon,
    MatDivider,
    MatMiniFabButton,
  ],
})
export class BlogPostItemComponent {
  private readonly router = inject(Router);

  readonly blogPost = input.required<BlogPost>();
  readonly index = input.required<number>();
  readonly isAdmin = input.required<boolean>();

  protected readonly imgDownloadPriority = computed(() => this.index() === 0 || this.index() === 1);

  onDownload(url: string) {
    window.open(url);
  }

  onEdit() {
    this.router.navigate([this.blogPost()._id, 'edit'], { replaceUrl: true });
  }
}
