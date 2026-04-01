import { HttpBackend, HttpClient, HttpErrorResponse } from '@angular/common/http';
import type { HttpHandlerFn, HttpInterceptorFn, HttpRequest } from '@angular/common/http';
import { inject } from '@angular/core';
import { catchError, switchMap, throwError } from 'rxjs';

import { TokenService } from './token.service';
import { mapAuthSessionErrorMessage } from './map-auth-session-error-message';

const AUTH_ROUTES = ['/api/auth/login', '/api/auth/refresh', '/api/auth/logout', '/api/auth/session'];

export const jwtInterceptor: HttpInterceptorFn = (request: HttpRequest<unknown>, next: HttpHandlerFn) => {
  const tokenService = inject(TokenService);
  const httpBackend = inject(HttpBackend);
  const rawHttpClient = new HttpClient(httpBackend);
  const isRelativeApiUrl = request.url.startsWith('/api');
  const isAbsoluteApiUrl = /^https?:\/\/[^/]+\/api(?:\/|$)/.test(request.url);
  const isAPIUrl = isRelativeApiUrl || isAbsoluteApiUrl;
  const isAuthRoute = AUTH_ROUTES.some((authRoute) => request.url.includes(authRoute));

  if (isAPIUrl) {
    request = request.clone({
      withCredentials: true,
    });
  }

  return next(request).pipe(
    catchError((error: unknown) => {
      if (!(error instanceof HttpErrorResponse) || error.status !== 401 || !isAPIUrl || isAuthRoute) {
        return throwError(() => error);
      }

      return rawHttpClient.post('/api/auth/refresh', {}, { withCredentials: true }).pipe(
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
