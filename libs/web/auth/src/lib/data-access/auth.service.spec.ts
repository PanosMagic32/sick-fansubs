import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { of } from 'rxjs';
import { vi } from 'vitest';

import { MatSnackBar } from '@angular/material/snack-bar';

import { TokenService } from '@web/shared';

import { AuthService } from './auth.service';

describe('AuthService', () => {
  let service: AuthService;
  let httpMock: HttpTestingController;

  const routerMock = {
    navigate: vi.fn().mockResolvedValue(true),
  };

  const snackBarMock = {
    open: vi.fn(),
  };

  const tokenServiceMock = {
    restoreSession: vi.fn(() => of(undefined)),
  };

  beforeEach(() => {
    vi.clearAllMocks();

    TestBed.configureTestingModule({
      providers: [
        AuthService,
        provideHttpClient(),
        provideHttpClientTesting(),
        { provide: Router, useValue: routerMock },
        { provide: MatSnackBar, useValue: snackBarMock },
        { provide: TokenService, useValue: tokenServiceMock },
      ],
    });

    service = TestBed.inject(AuthService);
    httpMock = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    httpMock.verify();
  });

  it('navigates home after successful login and session restore', async () => {
    service.login('tester', 'Password1!');

    const req = httpMock.expectOne('/api/auth/login');
    expect(req.request.method).toBe('POST');
    req.flush({ username: 'tester' });

    await Promise.resolve();
    await Promise.resolve();

    expect(tokenServiceMock.restoreSession).toHaveBeenCalledTimes(1);
    expect(routerMock.navigate).toHaveBeenCalledWith(['/'], { replaceUrl: true });
    expect(service.isLoading()).toBe(false);
  });

  it('navigates to login after successful signup', () => {
    service.signUp('new-user', 'new@example.com', 'Password1!', 'https://example.com/avatar.png');

    const req = httpMock.expectOne('/api/user');
    expect(req.request.method).toBe('POST');
    req.flush({ id: 'u-1', username: 'new-user', email: 'new@example.com', role: 'user', status: 'active', isAdmin: false });

    expect(snackBarMock.open).toHaveBeenCalledWith('Η εγγραφή ολοκληρώθηκε. Συνδεθείτε για να συνεχίσετε.', 'OK', {
      duration: 3000,
    });
    expect(routerMock.navigate).toHaveBeenCalledWith(['/auth/login'], { replaceUrl: true });
    expect(service.isLoading()).toBe(false);
  });

  it('shows conflict message on signup 409 error', () => {
    service.signUp('existing-user', 'existing@example.com', 'Password1!');

    const req = httpMock.expectOne('/api/user');
    req.flush({ message: 'Conflict' }, { status: 409, statusText: 'Conflict' });

    expect(snackBarMock.open).toHaveBeenCalledWith('Το email ή το όνομα χρήστη χρησιμοποιείται ήδη.', 'OK', {
      duration: 3000,
    });
    expect(service.isLoading()).toBe(false);
  });

  it('uses fallback auth error message for unknown errors', () => {
    const message = (service as unknown as { getAuthErrorMessage: (err: unknown) => string }).getAuthErrorMessage(
      new HttpErrorResponse({ status: 500, error: {} }),
    );

    expect(message).toBe('Προέκυψε σφάλμα πιστοποίησης. Δοκιμάστε ξανά.');
  });
});
