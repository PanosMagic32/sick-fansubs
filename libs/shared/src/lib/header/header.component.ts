import { Component, EventEmitter, OnInit, Output } from '@angular/core';

import { MenuService } from '../data-access/menu.service';

@Component({
  selector: 'sick-header',
  templateUrl: './header.component.html',
  styles: [],
})
export class HeaderComponent implements OnInit {
  @Output() sidenavToggle = new EventEmitter();

  isHandset$ = this.menuService.isHandset$;
  isMedium$ = this.menuService.isMedium$;
  isSmall$ = this.menuService.isSmall$;

  isOpenSearch = false;

  constructor(private menuService: MenuService) {}

  ngOnInit(): void {}

  onToggleSidenav() {
    this.sidenavToggle.emit();
  }
}
