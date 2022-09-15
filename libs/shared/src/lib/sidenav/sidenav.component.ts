import { Component, EventEmitter, OnInit, Output } from '@angular/core';

import { ConfigService } from '../data-access/config.service';

@Component({
  selector: 'sick-sidenav',
  templateUrl: './sidenav.component.html',
  styles: [],
})
export class SidenavComponent implements OnInit {
  @Output() sidenavClose = new EventEmitter();

  appVersion: string | undefined = '';

  constructor(private configService: ConfigService) {}

  ngOnInit(): void {
    this.appVersion = this.configService.APP_VERSION;
  }

  onSidenavClose() {
    this.sidenavClose.emit();
  }

  onGoToTracker() {
    window.open(this.configService.TRACKER_URL, '_blank');

    this.onSidenavClose();
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

      default:
        break;
    }
  }
}
