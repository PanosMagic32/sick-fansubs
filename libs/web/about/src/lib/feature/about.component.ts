import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import {
  MatCard,
  MatCardContent,
  MatCardFooter,
  MatCardHeader,
  MatCardSubtitle,
  MatCardTitle,
} from '@angular/material/card';
import { MatDivider } from '@angular/material/divider';
import { MatIcon } from '@angular/material/icon';
import { MatList, MatListItem, MatListItemIcon } from '@angular/material/list';

import { WebConfigService } from '@web/shared';

@Component({
  selector: 'sf-about',
  templateUrl: './about.component.html',
  styleUrl: './about.component.scss',
  imports: [
    MatCard,
    MatCardHeader,
    MatCardTitle,
    MatCardSubtitle,
    MatCardContent,
    MatCardFooter,
    MatDivider,
    MatList,
    MatListItem,
    MatListItemIcon,
    MatIcon,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AboutComponent {
  private readonly webConfigService = inject(WebConfigService);

  protected readonly facebookUrl = this.webConfigService.FACEBOOK_URL;
  protected readonly discordUrl = this.webConfigService.DISCORD_URL;
  protected readonly githubUrl = this.webConfigService.GITHUB_URL;
  protected readonly buyMeACofeeUrl = this.webConfigService.BUY_ME_A_COFFEE_URL;
  protected readonly trackerUrl = this.webConfigService.TRACKER_URL;
  protected readonly appVersion = this.webConfigService.APP_VERSION;
}
