import { Component, OnInit } from '@angular/core';
import { Router } from '@angular/router';

import { Project } from '../../data-access/project.interface';

@Component({
  selector: 'sick-project-detail',
  templateUrl: './project-detail.component.html',
  styles: [],
})
export class ProjectDetailComponent implements OnInit {
  project!: Project;

  constructor(private router: Router) {}

  ngOnInit(): void {
    const state = history.state.project;

    if (!state || state === undefined) {
      this.router.navigate(['/projects'], { replaceUrl: true });
      return;
    }

    this.project = state;
  }

  onDownload(link: string) {
    window.open(link, '_system');
  }
}
