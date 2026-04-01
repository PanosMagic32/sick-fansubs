import { provideHttpClient, withInterceptors } from '@angular/common/http';
import { type ApplicationConfig, inject, provideAppInitializer, provideZonelessChangeDetection } from '@angular/core';
import { provideRouter, withComponentInputBinding } from '@angular/router';
import { firstValueFrom } from 'rxjs';

import { jwtInterceptor, TokenService, WebConfigService } from '@web/shared';

import { environment } from '../environments/environment';
import { appRoutes } from './app.routes';

const appInitializerProvider = provideAppInitializer(() => {
  const webConfigService = inject(WebConfigService);
  const tokenService = inject(TokenService);
  const config = environment.config;

  webConfigService.API_URL = config.API_URL;
  webConfigService.APP_VERSION = config.APP_VERSION;
  webConfigService.FACEBOOK_URL = config.FACEBOOK_URL;
  webConfigService.DISCORD_URL = config.DISCORD_URL;
  webConfigService.GITHUB_URL = config.GITHUB_URL;
  webConfigService.BUY_ME_A_COFFEE_URL = config.BUY_ME_A_COFFEE_URL;
  webConfigService.TRACKER_URL = config.TRACKER_URL;

  return firstValueFrom(tokenService.restoreSession());
});

export const appConfig: ApplicationConfig = {
  providers: [
    provideZonelessChangeDetection(),
    provideHttpClient(withInterceptors([jwtInterceptor])),
    provideRouter(appRoutes, withComponentInputBinding()),
    appInitializerProvider,
  ],
};
