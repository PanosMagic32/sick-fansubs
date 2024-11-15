import { ChangeDetectionStrategy, Component, inject, output } from '@angular/core';
import { MatDivider } from '@angular/material/divider';
import { MatIcon } from '@angular/material/icon';
import { MatListItem, MatNavList } from '@angular/material/list';
import { MatToolbar } from '@angular/material/toolbar';
import { RouterLink, RouterLinkActive } from '@angular/router';

import { WebConfigService } from '../data-access/web-config.service';

@Component({
  selector: 'sf-sidenav',
  templateUrl: './sidenav.component.html',
  styleUrl: './sidenav.component.scss',
  standalone: true,
  imports: [RouterLink, RouterLinkActive, MatToolbar, MatNavList, MatListItem, MatIcon, MatDivider],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SidenavComponent {
  private readonly webConfigService = inject(WebConfigService);

  sidenavClose = output();

  appVersion = this.webConfigService.APP_VERSION;

  onSidenavClose() {
    this.sidenavClose.emit();
  }

  onGoToTracker() {
    window.open(this.webConfigService.TRACKER_URL, '_blank');

    this.onSidenavClose();
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

      default:
        break;
    }
  }
}
