import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';

import { TokenService } from './token.service';

describe('TokenService', () => {
  let service: TokenService;
  let httpMock: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [TokenService, provideHttpClient(), provideHttpClientTesting()],
    });

    service = TestBed.inject(TokenService);
    httpMock = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    httpMock.verify();
  });

  it('maps explicit role/status from session payload', () => {
    service.restoreSession().subscribe();

    const req = httpMock.expectOne('/api/auth/session');
    expect(req.request.method).toBe('GET');

    req.flush({
      sub: 'u-1',
      username: 'mod',
      email: 'mod@example.com',
      role: 'moderator',
      status: 'active',
    });

    expect(service.isAuthenticated()).toBe(true);
    expect(service.role()).toBe('moderator');
    expect(service.status()).toBe('active');
    expect(service.canManageContent()).toBe(false);
    expect(service.canAccessDashboard()).toBe(true);
  });

  it('defaults role/status when not provided', () => {
    service.restoreSession().subscribe();

    const req = httpMock.expectOne('/api/auth/session');
    req.flush({
      sub: 'u-2',
      username: 'legacy',
      email: 'legacy@example.com',
    });

    expect(service.role()).toBe('user');
    expect(service.status()).toBe('active');
    expect(service.canManageContent()).toBe(false);
    expect(service.hasAnyRole(['super-admin', 'admin'])).toBe(false);
  });

  it('clears session state on restore failure', () => {
    service.restoreSession().subscribe();

    const req = httpMock.expectOne('/api/auth/session');
    req.flush({}, { status: 401, statusText: 'Unauthorized' });

    expect(service.isAuthenticated()).toBe(false);
    expect(service.role()).toBe('user');
    expect(service.status()).toBe('active');
    expect(service.userId()).toBeNull();
  });
});
