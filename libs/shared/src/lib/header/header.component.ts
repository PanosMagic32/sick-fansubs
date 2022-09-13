import { Component, EventEmitter, Output } from '@angular/core';

import { MenuService } from '../data-access/menu.service';

@Component({
  selector: 'sick-header',
  templateUrl: './header.component.html',
  styles: [],
})
export class HeaderComponent {
  @Output() sidenavToggle = new EventEmitter();

  isHandset$ = this.menuService.isHandset$;
  isMedium$ = this.menuService.isMedium$;
  isSmall$ = this.menuService.isSmall$;

  isOpenSearch = false;

  constructor(private menuService: MenuService) {}

  onToggleSidenav() {
    this.sidenavToggle.emit();
  }

  onOpenSocial(url: string) {
    window.open(url, '_system');
  }

  onOpenTracker() {
    window.open('https://nyaa.si/user/Sick-Fansubs', '_system');
  }
}
