import { DatePipe, Location } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { ActivatedRoute, Router } from '@angular/router';

import { MatButton } from '@angular/material/button';
import { MatCard, MatCardAvatar, MatCardContent, MatCardHeader, MatCardImage, MatCardTitle } from '@angular/material/card';
import { MatDivider } from '@angular/material/divider';
import { MatIcon } from '@angular/material/icon';
import { MatMenuModule } from '@angular/material/menu';

import { TokenService } from '@web/shared';

import { ProjectsService } from '../../data-access/projects.service';
import type { Project, ProjectBatchDownloadLink } from '../../data-access/project.interface';

@Component({
  selector: 'sf-project-details',
  templateUrl: './project-details.component.html',
  styleUrl: './project-details.component.scss',
  imports: [
    MatCard,
    MatCardHeader,
    MatCardTitle,
    MatCardContent,
    MatCardImage,
    MatCardAvatar,
    MatDivider,
    MatIcon,
    MatButton,
    MatMenuModule,
    DatePipe,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export default class ProjectDetailsComponent {
  private readonly location = inject(Location);
  private readonly router = inject(Router);
  private readonly activatedRoute = inject(ActivatedRoute);
  private readonly tokenService = inject(TokenService);
  private readonly projectService = inject(ProjectsService);

  readonly id = input.required<string>();

  private readonly defaultAvatarPath = '/logo/logo.png';
  protected readonly isAdmin = this.tokenService.isAdmin;

  protected readonly project = this.projectService.getProjectById(this.id);
  protected readonly creatorAvatarUrl = computed(() => this.project.value()?.creator?.avatar || this.defaultAvatarPath);
  protected readonly createdByUsername = computed(() => this.project.value()?.creator?.username || 'kushoyarou');
  protected readonly editedByUsername = computed(
    () => this.project.value()?.updatedBy?.username || this.project.value()?.creator?.username || 'kushoyarou',
  );
  protected readonly hasBeenEdited = computed(() => {
    const project = this.project.value();
    if (!project?.updatedBy) {
      return false;
    }

    const updatedAt = project.updatedAt;
    const createdAt = project.dateTimeCreated;

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

  getBatchLinks(project: Project): ProjectBatchDownloadLink[] {
    return (project.batchDownloadLinks ?? [])
      .map((link, index) => {
        if (typeof link === 'string') {
          return {
            name: `Batch ${index + 1}`,
            downloadLinkTorrent: /^https?:\/\//i.test(link) ? link : '',
            downloadLink: /^magnet:\?xt=/i.test(link) ? link : '',
            downloadLink4kTorrent: '',
            downloadLink4k: '',
          };
        }

        const legacySingleUrl = (link as { url?: unknown }).url;
        const legacyUrlValue = typeof legacySingleUrl === 'string' ? legacySingleUrl.trim() : '';

        const torrent = String((link as { downloadLinkTorrent?: unknown }).downloadLinkTorrent ?? '').trim();
        const magnet = String((link as { downloadLink?: unknown }).downloadLink ?? '').trim();
        const torrent4k = String((link as { downloadLink4kTorrent?: unknown }).downloadLink4kTorrent ?? '').trim();
        const magnet4k = String((link as { downloadLink4k?: unknown }).downloadLink4k ?? '').trim();

        return {
          name: String(link.name ?? '').trim(),
          downloadLinkTorrent: torrent || (/^https?:\/\//i.test(legacyUrlValue) ? legacyUrlValue : ''),
          downloadLink: magnet || (/^magnet:\?xt=/i.test(legacyUrlValue) ? legacyUrlValue : ''),
          downloadLink4kTorrent: torrent4k,
          downloadLink4k: magnet4k,
        };
      })
      .filter((link) => link.downloadLinkTorrent || link.downloadLink);
  }

  onDownload(link: string) {
    window.open(link, '_system');
  }

  onBackToProjects() {
    this.location.back();
    return;
  }

  onEditProject() {
    this.router.navigate(['..', this.id(), 'edit'], {
      relativeTo: this.activatedRoute,
    });
  }
}
