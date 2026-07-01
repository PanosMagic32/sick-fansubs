import { HttpBackend, HttpClient, HttpErrorResponse } from '@angular/common/http';
import type { HttpHandlerFn, HttpInterceptorFn, HttpRequest } from '@angular/common/http';
import { inject } from '@angular/core';
import { catchError, finalize, Observable, share, switchMap, throwError } from 'rxjs';

import { TokenService } from './token.service';
import { WebConfigService } from './web-config.service';
import { mapAuthSessionErrorMessage } from './map-auth-session-error-message';

/** Auth endpoints as version-agnostic path suffixes — resolved via WebConfigService.resolveApiUrl(). */
const AUTH_PATHS = ['/auth/login', '/auth/refresh', '/auth/logout', '/auth/session'];

let refreshInProgress: Observable<unknown> | null = null;

export const jwtInterceptor: HttpInterceptorFn = (request: HttpRequest<unknown>, next: HttpHandlerFn) => {
  const tokenService = inject(TokenService);
  const webConfig = inject(WebConfigService);
  const httpBackend = inject(HttpBackend);
  const rawHttpClient = new HttpClient(httpBackend);

  const apiBase = webConfig.API_URL;
  if (!apiBase) return next(request);

  const isApiUrl = request.url.startsWith(apiBase) || request.url.startsWith('/api');
  if (isApiUrl) request = request.clone({ withCredentials: true });

  const isAuthRoute = AUTH_PATHS.some((path) => request.url === webConfig.resolveApiUrl(path));

  return next(request).pipe(
    catchError((error: unknown) => {
      if (!(error instanceof HttpErrorResponse) || error.status !== 401 || !isApiUrl || isAuthRoute) {
        return throwError(() => error);
      }

      // Deduplicate concurrent refresh requests
      if (!refreshInProgress) {
        refreshInProgress = rawHttpClient.post(webConfig.resolveApiUrl('/auth/refresh'), {}, { withCredentials: true }).pipe(
          finalize(() => {
            refreshInProgress = null;
          }),
          share(),
        );
      }

      return refreshInProgress.pipe(
        switchMap(() => next(request)),
        catchError((refreshError: unknown) => {
          tokenService.removeToken();

          if (refreshError instanceof HttpErrorResponse) {
            return throwError(
              () =>
                new HttpErrorResponse({
                  ...refreshError,
                  url: refreshError.url ?? undefined,
                  error: {
                    ...(typeof refreshError.error === 'object' && refreshError.error !== null ? refreshError.error : {}),
                    message: mapAuthSessionErrorMessage(refreshError),
                  },
                }),
            );
          }

          return throwError(() => refreshError);
        }),
      );
    }),
  );
};
