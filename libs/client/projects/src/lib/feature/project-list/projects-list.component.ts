import { Component, OnInit } from '@angular/core';
import { PageEvent } from '@angular/material/paginator';
import { tap } from 'rxjs';

import { ProjectsService } from '../../data-access/projects.service';

@Component({
  selector: 'sick-projects-list',
  templateUrl: './projects-list.component.html',
  styleUrls: ['./projects-list.component.scss'],
})
export class ProjectListComponent implements OnInit {
  totalProjects = 0;
  projectsPerPage = 5;
  currentPage = 0;
  pageSizeOptions = [5, 10];

  isLoading$ = this.projectsService.isLoading$;
  projects$ = this.projectsService.projects$.pipe(tap((response) => (this.totalProjects = response.count)));

  constructor(private projectsService: ProjectsService) {}

  ngOnInit(): void {
    this.projectsService.getProjects(this.projectsPerPage, this.currentPage);
  }

  onChangedPage(pageData: PageEvent) {
    this.currentPage = pageData.pageIndex;
    this.projectsPerPage = pageData.pageSize;

    this.projectsService.getProjects(this.projectsPerPage, this.currentPage);
  }
}
