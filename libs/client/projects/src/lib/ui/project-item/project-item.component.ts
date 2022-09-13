import { Component, Input, OnInit } from '@angular/core';
import { Router } from '@angular/router';

import { Project } from '../../data-access/project.interface';

@Component({
  selector: 'sick-project-item',
  templateUrl: './project-item.component.html',
  styles: [],
})
export class ProjectItemComponent implements OnInit {
  @Input() project!: Project;

  constructor(private router: Router) {}

  ngOnInit(): void {}

  onMore() {
    this.router.navigate(['/projects/detail'], { state: { project: this.project } });
  }
}
