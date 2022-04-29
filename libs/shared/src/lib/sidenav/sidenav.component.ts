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

  onGoToTracker() {
    window.open('https://nyaa.si/user/Sick-Fansubs', '_blank');

    this.onSidenavClose();
  }
}
