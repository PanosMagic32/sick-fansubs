import { AsyncPipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, output } from '@angular/core';
import { Router, RouterLink, RouterLinkActive } from '@angular/router';

import { MatIconButton } from '@angular/material/button';
import { MatIcon } from '@angular/material/icon';
import { MatToolbar, MatToolbarRow } from '@angular/material/toolbar';

import { MenuService } from '../../data-access/menu.service';
import { TokenService } from '../../data-access/token.service';
import { WebConfigService } from '../../data-access/web-config.service';

@Component({
  selector: 'sf-header',
  templateUrl: './header.component.html',
  styleUrl: './header.component.scss',
  imports: [RouterLink, RouterLinkActive, AsyncPipe, MatToolbar, MatToolbarRow, MatIcon, MatIconButton],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class HeaderComponent {
  private readonly menuService = inject(MenuService);
  private readonly webConfigService = inject(WebConfigService);
  private readonly tokenService = inject(TokenService);
  private readonly router = inject(Router);

  sidenavToggle = output();

  // isOpenSearch = signal(false);

  isHandset$ = this.menuService.isHandset$;
  isMedium$ = this.menuService.isMedium$;
  isSmall$ = this.menuService.isSmall$;

  isAdmin = computed(() => this.tokenService.isAdmin());

  onLogout() {
    this.tokenService.removeToken();
    this.router.navigate(['/'], { replaceUrl: true });
  }

  onToggleSidenav() {
    this.sidenavToggle.emit();
  }

  onOpenTracker() {
    window.open(this.webConfigService.TRACKER_URL, '_blank');
  }

  onOpenSocial(url: string) {
    switch (url) {
      case 'discord':
        window.open(this.webConfigService.DISCORD_URL, '_system');
        break;

      case 'facebook':
        window.open(this.webConfigService.FACEBOOK_URL, '_system');
        break;

      case 'github':
        window.open(this.webConfigService.GITHUB_URL, '_system');
        break;

      case 'buymeacoffee':
        window.open(this.webConfigService.BUY_ME_A_COFFEE_URL, '_system');
        break;

      default:
        break;
    }
  }
}
