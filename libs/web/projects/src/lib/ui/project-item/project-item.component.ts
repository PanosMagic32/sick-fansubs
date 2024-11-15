import { DatePipe, NgOptimizedImage } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { ActivatedRoute, Router } from '@angular/router';

import { MatButton, MatMiniFabButton } from '@angular/material/button';
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
  standalone: true,
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
    MatIcon,
    MatDivider,
    MatMiniFabButton,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ProjectItemComponent {
  private readonly router = inject(Router);
  private readonly activatedRoute = inject(ActivatedRoute);

  readonly project = input.required<Project>();
  readonly index = input.required<number>();
  readonly isAdmin = input.required<boolean>();

  protected readonly imgDownloadPriority = computed(() => this.index() === 0 || this.index() === 1);

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
}
