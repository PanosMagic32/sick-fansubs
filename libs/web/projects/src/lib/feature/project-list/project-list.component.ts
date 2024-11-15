import { ChangeDetectionStrategy, Component, computed, inject, type OnInit } from '@angular/core';
import { ActivatedRoute, Router } from '@angular/router';

import { MatFabButton } from '@angular/material/button';
import { MatIcon } from '@angular/material/icon';
import { MatPaginator, type PageEvent } from '@angular/material/paginator';
import { MatProgressBar } from '@angular/material/progress-bar';

import { NoContentComponent, TokenService } from '@web/shared';

import { ProjectsService } from '../../data-access/projects.service';
import { ProjectItemComponent } from '../../ui/project-item/project-item.component';

@Component({
  selector: 'sf-project-list',
  templateUrl: './project-list.component.html',
  standalone: true,
  imports: [NoContentComponent, ProjectItemComponent, MatProgressBar, MatIcon, MatPaginator, MatFabButton],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ProjectListComponent implements OnInit {
  private readonly projectsService = inject(ProjectsService);
  private readonly tokenService = inject(TokenService);
  private readonly router = inject(Router);
  private readonly activatedRoute = inject(ActivatedRoute);

  protected projectsPerPage = 5;
  protected currentPage = 0;
  protected pageSizeOptions = [5, 10, 20, 50, 100];

  protected readonly isAdmin = this.tokenService.isAdmin;
  protected readonly isLoading = this.projectsService.isLoading;
  protected readonly projects = this.projectsService.projects;

  protected readonly totalProjects = computed(() => this.projects().count);

  ngOnInit() {
    this.projectsService.getProjects(this.projectsPerPage, this.currentPage);
  }

  onChangedPage(pageData: PageEvent) {
    this.currentPage = pageData.pageIndex;
    this.projectsPerPage = pageData.pageSize;

    this.projectsService.getProjects(this.projectsPerPage, this.currentPage);
  }

  onCreateProject() {
    this.router.navigate(['create'], { relativeTo: this.activatedRoute });
  }
}
