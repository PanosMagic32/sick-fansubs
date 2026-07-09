import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { vi } from 'vitest';

import { MatSnackBar } from '@angular/material/snack-bar';

import { WebConfigService } from '@web/shared';

import { SearchService } from './search.service';

const TEST_API_URL = '/api/v1';

describe('SearchService', () => {
  let service: SearchService;
  let httpMock: HttpTestingController;
  let snackBarMock: { open: ReturnType<typeof vi.fn> };

  beforeEach(() => {
    snackBarMock = { open: vi.fn() };

    TestBed.configureTestingModule({
      providers: [
        SearchService,
        WebConfigService,
        provideHttpClient(),
        provideHttpClientTesting(),
        { provide: MatSnackBar, useValue: snackBarMock },
      ],
    });

    const webConfig = TestBed.inject(WebConfigService);
    webConfig.API_URL = TEST_API_URL;

    service = TestBed.inject(SearchService);
    httpMock = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    httpMock.verify();
  });

  it('calls the correct URL with query params', () => {
    service.search('test', 'all', 10, 0).subscribe();

    const req = httpMock.expectOne(`${TEST_API_URL}/search?searchTerm=test&type=all&pageSize=10&page=0`);
    expect(req.request.method).toBe('GET');
    req.flush({ results: [], total: 0 });
  });

  it('updates results and isLoading signals on success', () => {
    const mockResults = {
      results: [
        {
          _id: '1',
          title: 'Test Post',
          description: 'A test post description',
          thumbnail: '/img/test.png',
          dateTimeCreated: '2024-01-01',
          type: 'blog-post' as const,
        },
      ],
      total: 1,
    };

    service.search('test', 'blog-post', 5, 0).subscribe();

    const req = httpMock.expectOne(`${TEST_API_URL}/search?searchTerm=test&type=blog-post&pageSize=5&page=0`);
    req.flush(mockResults);

    expect(service.results()).toEqual(mockResults);
    expect(service.isLoading()).toBe(false);
  });

  it('handles errors gracefully, shows snackbar, and clears results', () => {
    service.search('bad-query', 'all', 10, 0).subscribe();

    const req = httpMock.expectOne(`${TEST_API_URL}/search?searchTerm=bad-query&type=all&pageSize=10&page=0`);
    req.flush({ message: 'Not Found' }, { status: 404, statusText: 'Not Found' });

    expect(snackBarMock.open).toHaveBeenCalledWith('Το στοιχείο δεν βρέθηκε.', 'OK', { duration: 3000 });
    expect(service.isLoading()).toBe(false);
    expect(service.results()).toEqual({ results: [], total: 0 });
  });

  it('sets isLoading to true when search starts', () => {
    expect(service.isLoading()).toBe(false);

    service.search('test', 'all', 10, 0).subscribe();

    expect(service.isLoading()).toBe(true);

    const req = httpMock.expectOne(`${TEST_API_URL}/search?searchTerm=test&type=all&pageSize=10&page=0`);
    req.flush({ results: [], total: 0 });
  });

  it('clearResults resets state to defaults', () => {
    const mockResults = {
      results: [
        {
          _id: '1',
          title: 'Test',
          description: 'Desc',
          thumbnail: '/img/test.png',
          dateTimeCreated: '2024-01-01',
          type: 'project' as const,
        },
      ],
      total: 1,
    };

    service.search('test', 'project', 10, 0).subscribe();

    const req = httpMock.expectOne(`${TEST_API_URL}/search?searchTerm=test&type=project&pageSize=10&page=0`);
    req.flush(mockResults);

    expect(service.results().total).toBe(1);

    service.clearResults();

    expect(service.results()).toEqual({ results: [], total: 0 });
    expect(service.isLoading()).toBe(false);
  });
});
