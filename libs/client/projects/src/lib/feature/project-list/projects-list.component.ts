import { Component, OnDestroy, OnInit } from '@angular/core';
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

  isLoading$ = this.projectsService.isLoading$;

  private projectSubscription!: Subscription;

  constructor(private projectsService: ProjectsService) {}

  ngOnInit(): void {
    this.projectsService.getProjects();

    this.projectSubscription = this.projectsService.getProjectUpdateListener().subscribe((res) => {
      // console.log(res);

      this.projects = res;
    });
  }

  ngOnDestroy(): void {
    if (this.projectSubscription) {
      this.projectSubscription.unsubscribe();
    }
  }
}
