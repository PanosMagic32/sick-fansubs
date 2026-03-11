import type { HttpHandlerFn, HttpInterceptorFn, HttpRequest } from '@angular/common/http';
import { inject } from '@angular/core';

import { TokenService } from './token.service';

export const jwtInterceptor: HttpInterceptorFn = (request: HttpRequest<unknown>, next: HttpHandlerFn) => {
  const token = inject(TokenService).getToken();
  const isRelativeApiUrl = request.url.startsWith('/api');
  const isAbsoluteApiUrl = /^https?:\/\/[^/]+\/api(?:\/|$)/.test(request.url);
  const isAPIUrl = isRelativeApiUrl || isAbsoluteApiUrl;

  if (token !== '' && isAPIUrl) {
    request = request.clone({
      setHeaders: {
        Authorization: `Bearer ${token}`,
      },
    });
  }

  return next(request);
};
