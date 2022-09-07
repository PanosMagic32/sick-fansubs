import { Component, EventEmitter, Output } from '@angular/core';

@Component({
  selector: 'sick-sidenav',
  templateUrl: './sidenav.component.html',
  styles: [],
})
export class SidenavComponent {
  @Output() sidenavClose = new EventEmitter();

  onSidenavClose() {
    this.sidenavClose.emit();
  }

  onGoToTracker() {
    window.open('https://nyaa.si/user/Sick-Fansubs', '_blank');

    this.onSidenavClose();
  }

  onOpenSocial(url: string) {
    window.open(url, '_system');
  }
}
