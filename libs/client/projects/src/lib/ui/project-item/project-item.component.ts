import { Component, Input } from '@angular/core';
import { Router } from '@angular/router';

import { Project } from '../../data-access/project.interface';

@Component({
  selector: 'sick-project-item',
  templateUrl: './project-item.component.html',
  styleUrls: ['./project-item.component.scss'],
})
export class ProjectItemComponent {
  @Input() project!: Project;
  @Input() index!: number;
  @Input() isAdmin!: boolean;

  constructor(private readonly router: Router) {}

  onMore() {
    this.router.navigate(['/', 'projects', this.project._id]);
  }

  onEdit() {
    this.router.navigate(['/', 'projects', this.project._id, 'edit'], { replaceUrl: true });
  }
}
