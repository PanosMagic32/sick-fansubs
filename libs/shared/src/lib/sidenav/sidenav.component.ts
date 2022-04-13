import { Component, EventEmitter, OnInit, Output } from '@angular/core';

@Component({
  selector: 'sick-sidenav',
  templateUrl: './sidenav.component.html',
  styles: [],
})
export class SidenavComponent implements OnInit {
  @Output() sidenavClose = new EventEmitter();

  constructor() {}

  ngOnInit(): void {}

  onSidenavClose() {
    this.sidenavClose.emit();
  }
}
