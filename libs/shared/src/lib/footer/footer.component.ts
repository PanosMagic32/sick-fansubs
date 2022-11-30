import { Component, OnInit } from '@angular/core';

import { ConfigService } from '../data-access/config.service';

@Component({
    selector: 'sick-footer',
    templateUrl: './footer.component.html',
    styleUrls: ['./footer.component.scss'],
})
export class FooterComponent implements OnInit {
    appVersion: string | undefined = '';

    constructor(private configService: ConfigService) {}

    ngOnInit(): void {
        this.appVersion = this.configService.APP_VERSION;
    }
}
