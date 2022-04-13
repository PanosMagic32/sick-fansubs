import { Component, EventEmitter, OnInit, Output } from '@angular/core';

@Component({
  selector: 'sick-header',
  templateUrl: './header.component.html',
  styles: [],
})
export class HeaderComponent implements OnInit {
  @Output() sidenavToggle = new EventEmitter();

  isOpenSearch = false;

  constructor() {}

  ngOnInit(): void {}

  onToggleSidenav() {
    this.sidenavToggle.emit();
  }
}
