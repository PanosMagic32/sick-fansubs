import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { ActivatedRoute, Router } from '@angular/router';
import { map, tap, switchMap } from 'rxjs';

import { MatFabButton } from '@angular/material/button';
import { MatIcon } from '@angular/material/icon';
import { MatPaginator, type PageEvent } from '@angular/material/paginator';
import { MatProgressBar } from '@angular/material/progress-bar';

import { NoContentComponent, TokenService } from '@web/shared';

import { ProjectsService } from '../../data-access/projects.service';
import { ProjectItemComponent } from '../../ui/project-item/project-item.component';

const DEFAULT_PAGE = 1;
const DEFAULT_PAGE_SIZE = 5;
const PAGE_SIZE_OPTIONS = [5, 10, 20, 50, 100];

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

  protected readonly currentPage = signal(DEFAULT_PAGE);
  protected readonly pageSize = signal(DEFAULT_PAGE_SIZE);
  protected readonly pageSizeOptions = signal(PAGE_SIZE_OPTIONS);

  protected readonly isAdmin = this.tokenService.isAdmin;
  protected readonly isLoading = this.projectsService.isLoading;
  protected readonly projects = this.projectsService.projects;

  protected readonly totalProjects = computed(() => this.projects().count);

  constructor() {
    this.activatedRoute.queryParamMap
      .pipe(
        takeUntilDestroyed(),
        map((params) => ({
          page: +(params.get('page') ?? this.currentPage()),
          pageSize: +(params.get('pageSize') ?? this.pageSize()),
        })),
        tap(({ page, pageSize }) => {
          this.currentPage.set(page);
          this.pageSize.set(pageSize);
        }),
        switchMap(({ page, pageSize }) => this.projectsService.getProjects(pageSize, page - 1)),
      )
      .subscribe();
  }

  onCreateProject() {
    this.router.navigate(['create'], { relativeTo: this.activatedRoute });
  }

  onPageChange(event: PageEvent) {
    this.handleQueryParamsNavigation({ page: event.pageIndex + 1, pageSize: event.pageSize });
  }

  private handleQueryParamsNavigation(queryParams: Partial<{ page: number; pageSize: number }>) {
    this.router.navigate([], {
      queryParams,
      queryParamsHandling: 'merge',
      relativeTo: this.activatedRoute,
    });
  }
}
