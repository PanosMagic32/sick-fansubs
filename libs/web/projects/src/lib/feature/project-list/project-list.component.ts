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

import { ProjectsService } from '../../data-access/projects.service';
import { ProjectItemComponent } from '../../ui/project-item/project-item.component';

const DEFAULT_PAGE = 1;
const DEFAULT_PAGE_SIZE = 10;
const PAGE_SIZE_OPTIONS = [10, 20, 50, 100];

@Component({
  selector: 'sf-project-list',
  templateUrl: './project-list.component.html',
  styleUrl: './project-list.component.scss',
  imports: [NoContentComponent, ProjectItemComponent, MatProgressBar, MatIcon, MatPaginator, MatFabButton],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ProjectListComponent {
  private readonly projectsService = inject(ProjectsService);
  private readonly tokenService = inject(TokenService);
  private readonly router = inject(Router);
  private readonly activatedRoute = inject(ActivatedRoute);
  private readonly snackBar = inject(MatSnackBar);
  private readonly addFavoriteRequest = signal<string | null>(null);
  private readonly removeFavoriteRequest = signal<string | null>(null);
  private readonly favoriteProjectIdsState = signal<string[]>([]);

  protected readonly currentPage = signal(DEFAULT_PAGE);
  protected readonly pageSize = signal(DEFAULT_PAGE_SIZE);
  protected readonly pageSizeOptions = signal(PAGE_SIZE_OPTIONS);

  protected readonly canManageContent = this.tokenService.canManageContent;
  protected readonly userId = this.tokenService.userId;
  protected readonly activeUserId = computed(() => (this.tokenService.isValidToken() ? this.userId() : null));
  protected readonly isAuthenticated = computed(() => Boolean(this.activeUserId()));
  protected readonly favoriteProjectIds = this.favoriteProjectIdsState.asReadonly();
  protected readonly favoriteActionPendingProjectId = computed(
    () => this.addFavoriteRequest() ?? this.removeFavoriteRequest(),
  );

  protected readonly projects = this.projectsService.getProjects(this.pageSize, this.currentPage);
  protected readonly totalProjects = computed(() => this.projects.value()?.count ?? 0);
  protected readonly favoriteProjectIdsResource = this.projectsService.getFavoriteProjectIds(this.activeUserId);
  protected readonly addFavoriteResource = this.projectsService.addFavoriteProject(
    this.activeUserId,
    this.addFavoriteRequest,
  );
  protected readonly removeFavoriteResource = this.projectsService.removeFavoriteProject(
    this.activeUserId,
    this.removeFavoriteRequest,
  );

  constructor() {
    this.activatedRoute.queryParamMap
      .pipe(
        takeUntilDestroyed(),
        map((params) => ({
          page: +(params.get('page') ?? DEFAULT_PAGE),
          pageSize: +(params.get('pageSize') ?? DEFAULT_PAGE_SIZE),
        })),
        tap(({ page, pageSize }) => {
          this.currentPage.set(page);
          this.pageSize.set(pageSize);
        }),
      )
      .subscribe();

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

  onCreateProject() {
    this.router.navigate(['create'], { relativeTo: this.activatedRoute });
  }

  onPageChange(event: PageEvent) {
    this.handleQueryParamsNavigation({ page: event.pageIndex + 1, pageSize: event.pageSize });
  }

  onToggleFavorite(projectId: string | undefined) {
    if (!projectId) return;

    if (!this.activeUserId()) {
      this.router.navigate(['/auth/login']);
      return;
    }

    if (this.favoriteActionPendingProjectId()) return;

    if (this.isFavorite(projectId)) {
      this.removeFavoriteRequest.set(projectId);
      return;
    }

    this.addFavoriteRequest.set(projectId);
  }

  protected isFavorite(projectId: string | undefined): boolean {
    if (!projectId) {
      return false;
    }

    return this.favoriteProjectIds().includes(projectId);
  }

  private handleQueryParamsNavigation(queryParams: Partial<{ page: number; pageSize: number }>) {
    this.router.navigate([], {
      queryParams,
      queryParamsHandling: 'merge',
      relativeTo: this.activatedRoute,
    });
  }
}
