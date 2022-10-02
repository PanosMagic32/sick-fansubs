import { Component, Input } from '@angular/core';
import { Router } from '@angular/router';

import { Project } from '../../data-access/project.interface';

@Component({
  selector: 'sick-project-item',
  templateUrl: './project-item.component.html',
  styles: [],
})
export class ProjectItemComponent {
  @Input() project!: Project;

  constructor(private router: Router) {}

  onMore() {
    this.router.navigate(['/', 'projects', this.project._id]);
  }
}
