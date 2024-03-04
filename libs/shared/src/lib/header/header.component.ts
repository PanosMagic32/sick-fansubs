import { Component, EventEmitter, Output } from '@angular/core';

import { TokenService } from '@sick/client/auth';

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

  isAdmin$ = this.tokenService.isAdmin$;

  isOpenSearch = false;

  constructor(
    private readonly menuService: MenuService,
    private readonly configService: ConfigService,
    private readonly tokenService: TokenService,
  ) {}

  onLogout() {
    this.tokenService.removeToken();
  }

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

      case 'buymeacoffee':
        window.open(this.configService.BUY_ME_A_COFFEE_URL, '_system');
        break;

      default:
        break;
    }
  }
}
