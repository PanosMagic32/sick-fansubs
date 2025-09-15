import { HttpClient, provideHttpClient, withInterceptors } from '@angular/common/http';
import { type ApplicationConfig, inject, provideAppInitializer, provideZonelessChangeDetection } from '@angular/core';
import { provideRouter, withComponentInputBinding } from '@angular/router';
import { catchError, of, tap } from 'rxjs';

import { jwtInterceptor, WebConfigService } from '@web/shared';

import { environment } from '../environments/environment';
import { appRoutes } from './app.routes';

const appInitializerProvider = provideAppInitializer(() => {
  const initializerFn = (() => {
    const webConfigService = inject(WebConfigService);
    const http = inject(HttpClient);
    return () =>
      new Promise((resolve) => {
        if (environment.production) {
          // load config for a deployed app
          http
            .get('.config.json')
            .pipe(
              tap((config: any) => {
                webConfigService.API_URL = config.API_URL;
                webConfigService.APP_VERSION = config.APP_VERSION;
                webConfigService.FACEBOOK_URL = config.FACEBOOK_URL;
                webConfigService.DISCORD_URL = config.DISCORD_URL;
                webConfigService.GITHUB_URL = config.GITHUB_URL;
                webConfigService.BUY_ME_A_COFFEE_URL = config.BUY_ME_A_COFFEE_URL;
                webConfigService.TRACKER_URL = config.TRACKER_URL;
                resolve(true);
              }),
              catchError(() => {
                webConfigService.API_URL = 'https://sickfansubs.com/api';
                resolve(true);
                return of(null);
              }),
            )
            .subscribe();
        } else {
          // load config for a local app
          // eslint-disable-next-line @typescript-eslint/no-var-requires
          const config = require('.config.json');
          webConfigService.API_URL = 'http://localhost:3333/api';
          webConfigService.APP_VERSION = config.APP_VERSION;
          webConfigService.FACEBOOK_URL = config.FACEBOOK_URL;
          webConfigService.DISCORD_URL = config.DISCORD_URL;
          webConfigService.GITHUB_URL = config.GITHUB_URL;
          webConfigService.BUY_ME_A_COFFEE_URL = config.BUY_ME_A_COFFEE_URL;
          webConfigService.TRACKER_URL = config.TRACKER_URL;
          resolve(true);
        }
      });
  })();

  return initializerFn();
});

export const appConfig: ApplicationConfig = {
  providers: [
    provideZonelessChangeDetection(),
    provideHttpClient(withInterceptors([jwtInterceptor])),
    provideRouter(appRoutes, withComponentInputBinding()),
    appInitializerProvider,
  ],
};
