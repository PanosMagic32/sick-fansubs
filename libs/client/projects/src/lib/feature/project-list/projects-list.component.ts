import { Component, OnInit } from '@angular/core';

import { ProjectsService } from '../../data-access/projects.service';

@Component({
  selector: 'sick-projects-list',
  templateUrl: './projects-list.component.html',
  styles: [],
})
export class ProjectListComponent implements OnInit {
  projects$ = this.projectsService.getProjects();

  constructor(private projectsService: ProjectsService) {}

  ngOnInit(): void {}
}
