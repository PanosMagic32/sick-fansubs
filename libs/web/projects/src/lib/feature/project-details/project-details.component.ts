import { Location } from '@angular/common';
import { ChangeDetectionStrategy, Component, inject, input, type OnInit } from '@angular/core';

import { MatButton } from '@angular/material/button';
import { MatCard, MatCardAvatar, MatCardContent, MatCardHeader, MatCardImage, MatCardTitle } from '@angular/material/card';
import { MatDivider } from '@angular/material/divider';
import { MatIcon } from '@angular/material/icon';

import { ProjectsService } from '../../data-access/projects.service';

@Component({
  selector: 'sf-project-details',
  templateUrl: './project-details.component.html',
  styleUrl: './project-details.component.scss',
  standalone: true,
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
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export default class ProjectDetailsComponent implements OnInit {
  private readonly location = inject(Location);
  private readonly projectService = inject(ProjectsService);

  readonly id = input.required<string>();

  project = this.projectService.selectedProject;

  ngOnInit() {
    if (!this.id()) this.onBackToProjects();

    this.projectService.getProjectById(this.id());
  }

  onDownload(link: string) {
    window.open(link, '_system');
  }

  onBackToProjects() {
    this.location.back();
    return;
  }
}
