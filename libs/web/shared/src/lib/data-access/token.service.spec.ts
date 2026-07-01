import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';

import { WebConfigService } from './web-config.service';
import { TokenService } from './token.service';

const TEST_API_URL = '/api/v1';

describe('TokenService', () => {
  let service: TokenService;
  let httpMock: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [TokenService, WebConfigService, provideHttpClient(), provideHttpClientTesting()],
    });

    const webConfig = TestBed.inject(WebConfigService);
    webConfig.API_URL = TEST_API_URL;

    service = TestBed.inject(TokenService);
    httpMock = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    httpMock.verify();
  });

  it('maps explicit role/status from session payload', () => {
    service.restoreSession().subscribe();

    const req = httpMock.expectOne(`${TEST_API_URL}/auth/session`);
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

    const req = httpMock.expectOne(`${TEST_API_URL}/auth/session`);
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

    const req = httpMock.expectOne(`${TEST_API_URL}/auth/session`);
    req.flush({}, { status: 401, statusText: 'Unauthorized' });

    expect(service.isAuthenticated()).toBe(false);
    expect(service.role()).toBe('user');
    expect(service.status()).toBe('active');
    expect(service.userId()).toBeNull();
  });
});
