import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';

import { WebConfigService } from '@web/shared';

import { HealthService } from './health.service';

const TEST_API_URL = '/api/v1';

describe('HealthService', () => {
  let service: HealthService;
  let httpMock: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [HealthService, WebConfigService, provideHttpClient(), provideHttpClientTesting()],
    });

    const webConfig = TestBed.inject(WebConfigService);
    webConfig.API_URL = TEST_API_URL;

    service = TestBed.inject(HealthService);
    httpMock = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    httpMock.verify();
  });

  it('returns true when API responds with {status: "ok"}', () => {
    let result: boolean | undefined;

    service.checkHealth().subscribe((healthy) => {
      result = healthy;
    });

    const req = httpMock.expectOne(`${TEST_API_URL}/health`);
    expect(req.request.method).toBe('GET');
    req.flush({ status: 'ok' });

    expect(result).toBe(true);
  });

  it('returns false on HTTP error', () => {
    let result: boolean | undefined;

    service.checkHealth().subscribe((healthy) => {
      result = healthy;
    });

    const req = httpMock.expectOne(`${TEST_API_URL}/health`);
    req.flush({ message: 'Service Unavailable' }, { status: 503, statusText: 'Service Unavailable' });

    expect(result).toBe(false);
  });

  it('returns false on network error (status 0)', () => {
    let result: boolean | undefined;

    service.checkHealth().subscribe((healthy) => {
      result = healthy;
    });

    const req = httpMock.expectOne(`${TEST_API_URL}/health`);
    req.error(new ProgressEvent('error'));

    expect(result).toBe(false);
  });

  it('returns false when status is not "ok"', () => {
    let result: boolean | undefined;

    service.checkHealth().subscribe((healthy) => {
      result = healthy;
    });

    const req = httpMock.expectOne(`${TEST_API_URL}/health`);
    req.flush({ status: 'error' });

    expect(result).toBe(false);
  });
});
