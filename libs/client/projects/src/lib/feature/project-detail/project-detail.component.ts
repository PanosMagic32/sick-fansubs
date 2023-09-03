import { Component, OnInit } from '@angular/core';
import { ActivatedRoute } from '@angular/router';
import { Location } from '@angular/common';
import { Observable } from 'rxjs';

import { Project } from '../../data-access/project.interface';
import { ProjectsService } from '../../data-access/projects.service';

@Component({
  selector: 'sick-project-detail',
  templateUrl: './project-detail.component.html',
  styleUrls: ['./project-detail.component.scss'],
})
export class ProjectDetailComponent implements OnInit {
  project$!: Observable<Project>;

  constructor(
    private route: ActivatedRoute,
    private location: Location,
    private projectService: ProjectsService,
  ) {}

  ngOnInit(): void {
    const projectId = this.route.snapshot.paramMap.get('id');

    if (!projectId || projectId === undefined) {
      this.onBackToProjects();
    } else {
      this.project$ = this.projectService.getProjectById(projectId);
    }
  }

  onDownload(link: string) {
    window.open(link, '_system');
  }

  onBackToProjects() {
    this.location.back();
    return;
  }
}
