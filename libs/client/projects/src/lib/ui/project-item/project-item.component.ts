import { Component, OnInit } from '@angular/core';
import { Router } from '@angular/router';

@Component({
  selector: 'sick-project-item',
  templateUrl: './project-item.component.html',
  styles: [],
})
export class ProjectItemComponent implements OnInit {
  constructor(private router: Router) {}

  ngOnInit(): void {}

  onMore() {
    this.router.navigate(['/projects/detail']);
  }
}
