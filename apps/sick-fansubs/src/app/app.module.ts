import { APP_INITIALIZER, inject, NgModule } from '@angular/core';
import { BrowserModule } from '@angular/platform-browser';
import { BrowserAnimationsModule } from '@angular/platform-browser/animations';
import { HttpClient, HttpClientModule, HTTP_INTERCEPTORS } from '@angular/common/http';
import { catchError, of, tap } from 'rxjs';

import { MaterialModule } from '@sick/material';
import { ConfigService, SharedModule } from '@sick/shared';
import { ClientShellModule } from '@sick/client/shell';
import { JwtInterceptor } from '@sick/client/auth';

import { AppComponent } from './app.component';
import { AppRoutingModule } from './app-routing.module';
import { environment } from '../environments/environment';

@NgModule({
  declarations: [AppComponent],
  imports: [
    BrowserModule,
    BrowserAnimationsModule,
    HttpClientModule,
    AppRoutingModule,
    MaterialModule,
    SharedModule,
    ClientShellModule,
  ],
  providers: [
    {
      provide: APP_INITIALIZER,
      useFactory: () => {
        const configService = inject(ConfigService);
        const http = inject(HttpClient);

        return () =>
          new Promise((resolve) => {
            if (environment.production) {
              // load config for a deployed app
              http
                .get('.config.json')
                .pipe(
                  tap((config: any) => {
                    configService.API_URL = config.API_URL;
                    configService.APP_VERSION = config.APP_VERSION;
                    configService.FACEBOOK_URL = config.FACEBOOK_URL;
                    configService.DISCORD_URL = config.DISCORD_URL;
                    configService.GITHUB_URL = config.GITHUB_URL;
                    configService.BUY_ME_A_COFFEE_URL = config.BUY_ME_A_COFFEE_URL;
                    configService.TRACKER_URL = config.TRACKER_URL;

                    resolve(true);
                  }),
                  catchError(() => {
                    configService.API_URL = 'https://sickfansubs.com/api';

                    resolve(true);
                    return of(null);
                  }),
                )
                .subscribe();
            } else {
              // load config for a local app
              // eslint-disable-next-line @typescript-eslint/no-var-requires
              const config = require('.config.json');

              configService.API_URL = 'http://localhost:3333/api';
              configService.APP_VERSION = config.APP_VERSION;
              configService.FACEBOOK_URL = config.FACEBOOK_URL;
              configService.DISCORD_URL = config.DISCORD_URL;
              configService.GITHUB_URL = config.GITHUB_URL;
              configService.BUY_ME_A_COFFEE_URL = config.BUY_ME_A_COFFEE_URL;
              configService.TRACKER_URL = config.TRACKER_URL;

              resolve(true);
            }
          });
      },
      multi: true,
    },
    { provide: HTTP_INTERCEPTORS, useClass: JwtInterceptor, multi: true },
  ],
  bootstrap: [AppComponent],
})
export class AppModule {}
