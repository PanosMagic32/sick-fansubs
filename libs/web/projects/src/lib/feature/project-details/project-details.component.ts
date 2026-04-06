import { DatePipe, Location } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, effect, inject, input, signal } from '@angular/core';
import { ActivatedRoute, Router } from '@angular/router';

import { MatButton } from '@angular/material/button';
import { MatCard, MatCardAvatar, MatCardContent, MatCardHeader, MatCardImage, MatCardTitle } from '@angular/material/card';
import { MatDivider } from '@angular/material/divider';
import { MatIcon } from '@angular/material/icon';
import { MatMenuModule } from '@angular/material/menu';
import { MatSnackBar } from '@angular/material/snack-bar';

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
  private readonly snackBar = inject(MatSnackBar);

  readonly id = input.required<string>();
  private readonly addFavoriteRequest = signal<string | null>(null);
  private readonly removeFavoriteRequest = signal<string | null>(null);
  private readonly favoriteProjectIdsState = signal<string[]>([]);

  private readonly defaultAvatarPath = '/logo/logo.png';
  protected readonly canManageContent = this.tokenService.canManageContent;
  protected readonly userId = this.tokenService.userId;
  protected readonly activeUserId = computed(() => (this.tokenService.isValidToken() ? this.userId() : null));
  protected readonly isAuthenticated = computed(() => Boolean(this.activeUserId()));
  protected readonly favoriteActionPending = computed(() =>
    Boolean(this.addFavoriteRequest() ?? this.removeFavoriteRequest()),
  );

  protected readonly project = this.projectService.getProjectById(this.id);
  protected readonly favoriteProjectIdsResource = this.projectService.getFavoriteProjectIds(this.activeUserId);
  protected readonly addFavoriteResource = this.projectService.addFavoriteProject(
    this.activeUserId,
    this.addFavoriteRequest,
  );
  protected readonly removeFavoriteResource = this.projectService.removeFavoriteProject(
    this.activeUserId,
    this.removeFavoriteRequest,
  );
  protected readonly creatorAvatarUrl = computed(() => this.project.value()?.creator?.avatar || this.defaultAvatarPath);
  protected readonly createdByUsername = computed(() => this.project.value()?.creator?.username || 'Kushoyarou');
  protected readonly isFavorite = computed(() => {
    const projectId = this.project.value()?._id;
    return Boolean(projectId && this.favoriteProjectIdsState().includes(projectId));
  });
  protected readonly editedByUsername = computed(
    () => this.project.value()?.updatedBy?.username || this.project.value()?.creator?.username || 'Kushoyarou',
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

  constructor() {
    effect(() => {
      if (!this.activeUserId()) {
        this.favoriteProjectIdsState.set([]);
      }
    });

    effect(() => {
      if (!this.favoriteProjectIdsResource.hasValue()) {
        return;
      }

      const favoriteIds = this.favoriteProjectIdsResource.value()?.favoriteProjectIds;
      if (favoriteIds) {
        this.favoriteProjectIdsState.set([...favoriteIds]);
      }
    });

    effect(() => {
      const error = this.addFavoriteResource.error();
      if (error) {
        this.snackBar.open('Αποτυχία προσθήκης project στα αγαπημένα.', 'OK', { duration: 3000 });
        this.addFavoriteRequest.set(null);
        return;
      }

      if (!this.addFavoriteResource.hasValue()) {
        return;
      }

      const favoriteIds = this.addFavoriteResource.value()?.favoriteProjectIds;
      if (favoriteIds) {
        this.favoriteProjectIdsState.set([...favoriteIds]);
        this.snackBar.open('Το project προστέθηκε στα αγαπημένα.', 'OK', { duration: 3000 });
        this.addFavoriteRequest.set(null);
      }
    });

    effect(() => {
      const error = this.removeFavoriteResource.error();
      if (error) {
        this.snackBar.open('Αποτυχία αφαίρεσης project από τα αγαπημένα.', 'OK', { duration: 3000 });
        this.removeFavoriteRequest.set(null);
        return;
      }

      if (!this.removeFavoriteResource.hasValue()) {
        return;
      }

      const favoriteIds = this.removeFavoriteResource.value()?.favoriteProjectIds;
      if (favoriteIds) {
        this.favoriteProjectIdsState.set([...favoriteIds]);
        this.snackBar.open('Το project αφαιρέθηκε από τα αγαπημένα.', 'OK', { duration: 3000 });
        this.removeFavoriteRequest.set(null);
      }
    });
  }

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

  onToggleFavorite() {
    const projectId = this.project.value()?._id;
    if (!projectId) {
      return;
    }

    if (!this.activeUserId()) {
      this.router.navigate(['/auth/login']);
      return;
    }

    if (this.favoriteActionPending()) {
      return;
    }

    if (this.isFavorite()) {
      this.removeFavoriteRequest.set(projectId);
      return;
    }

    this.addFavoriteRequest.set(projectId);
  }
}
