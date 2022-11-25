import { Component, OnDestroy, OnInit } from '@angular/core';
import { PageEvent } from '@angular/material/paginator';
import { Subscription } from 'rxjs';
import { Project } from '../../data-access/project.interface';

import { ProjectsService } from '../../data-access/projects.service';

@Component({
  selector: 'sick-projects-list',
  templateUrl: './projects-list.component.html',
  styles: [],
})
export class ProjectListComponent implements OnInit, OnDestroy {
  projects: Project[] = [];
  totalProjects = 0;
  projectsPerPage = 5;
  currentPage = 0;
  pageSizeOptions = [5, 10];

  isLoading$ = this.projectsService.isLoading$;

  private projectSubscription!: Subscription;

  constructor(private projectsService: ProjectsService) {}

  ngOnInit(): void {
    this.projectsService.getProjects(this.projectsPerPage, this.currentPage);

    this.projectSubscription = this.projectsService.getProjectUpdateListener().subscribe((res) => {
      // console.log(res);
      this.projects = res.projects;
      this.totalProjects = res.count;
    });
  }

  ngOnDestroy(): void {
    if (this.projectSubscription) {
      this.projectSubscription.unsubscribe();
    }
  }

  onChangedPage(pageData: PageEvent) {
    this.currentPage = pageData.pageIndex;
    this.projectsPerPage = pageData.pageSize;

    this.projectsService.getProjects(this.projectsPerPage, this.currentPage);
  }
}
