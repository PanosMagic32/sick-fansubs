import { Component, EventEmitter, Output } from '@angular/core';

import { ConfigService } from '../data-access/config.service';
import { MenuService } from '../data-access/menu.service';

@Component({
  selector: 'sick-header',
  templateUrl: './header.component.html',
  styleUrls: ['./header.component.scss'],
})
export class HeaderComponent {
  @Output() sidenavToggle = new EventEmitter();

  isHandset$ = this.menuService.isHandset$;
  isMedium$ = this.menuService.isMedium$;
  isSmall$ = this.menuService.isSmall$;

  isOpenSearch = false;

  constructor(
    private menuService: MenuService,
    private configService: ConfigService,
  ) {}

  onToggleSidenav() {
    this.sidenavToggle.emit();
  }

  onOpenTracker() {
    window.open(this.configService.TRACKER_URL, '_blank');
  }

  onOpenSocial(url: string) {
    switch (url) {
      case 'discord':
        window.open(this.configService.DISCORD_URL, '_system');
        break;

      case 'facebook':
        window.open(this.configService.FACEBOOK_URL, '_system');
        break;

      case 'github':
        window.open(this.configService.GITHUB_URL, '_system');
        break;

      case 'paypal':
        window.open(this.configService.PAYPAL_URL, '_system');
        break;

      default:
        break;
    }
  }
}
