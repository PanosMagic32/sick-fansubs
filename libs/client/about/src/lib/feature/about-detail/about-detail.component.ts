import { Component, OnInit } from '@angular/core';

import { ConfigService } from '@sick/shared';

@Component({
    selector: 'sick-about-detail',
    templateUrl: './about-detail.component.html',
    styleUrls: ['./about-detail.component.scss'],
})
export class AboutDetailComponent implements OnInit {
    facebookUrl: string | undefined = '';
    discordUrl: string | undefined = '';
    githubUrl: string | undefined = '';
    trackerUrl: string | undefined = '';
    appVersion: string | undefined = '';

    constructor(private configService: ConfigService) {}

    ngOnInit(): void {
        this.facebookUrl = this.configService.FACEBOOK_URL;
        this.discordUrl = this.configService.DISCORD_URL;
        this.githubUrl = this.configService.GITHUB_URL;
        this.trackerUrl = this.configService.TRACKER_URL;
        this.appVersion = this.configService.APP_VERSION;
    }
}
