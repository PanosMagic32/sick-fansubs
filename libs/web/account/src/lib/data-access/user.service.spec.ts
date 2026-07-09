import { ApplicationRef, signal } from '@angular/core';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';

import { WebConfigService } from '@web/shared';

import { UserService } from './user.service';

const TEST_API_URL = '/api/v1';

/**
 * Flush pending Angular effects by triggering change detection.
 * In zoneless tests, `httpResource` schedules requests internally via effects
 * that only fire when `ApplicationRef.tick()` runs.
 */
async function flushEffects(): Promise<void> {
  const appRef = TestBed.inject(ApplicationRef);
  appRef.tick();
  await Promise.resolve();
}

describe('UserService', () => {
  let service: UserService;
  let httpMock: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [UserService, WebConfigService, provideHttpClient(), provideHttpClientTesting()],
    });

    const webConfig = TestBed.inject(WebConfigService);
    webConfig.API_URL = TEST_API_URL;

    service = TestBed.inject(UserService);
    httpMock = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    httpMock.verify();
  });

  it('getUserProfile returns correct URL with user ID', async () => {
    const userId = signal('user-123');

    const resource = TestBed.runInInjectionContext(() => service.getUserProfile(userId));

    await flushEffects();

    const req = httpMock.expectOne(`${TEST_API_URL}/user/user-123`);
    expect(req.request.method).toBe('GET');

    req.flush({
      id: 'user-123',
      username: 'testuser',
      email: 'test@example.com',
      favoriteBlogPostIds: [],
      favoriteProjectIds: [],
      createdBlogPostIds: [],
    });

    await flushEffects();

    expect(resource.value()).toEqual({
      id: 'user-123',
      username: 'testuser',
      email: 'test@example.com',
      favoriteBlogPostIds: [],
      favoriteProjectIds: [],
      createdBlogPostIds: [],
    });
  });

  it('updateUserProfile only fires when body signal is non-null', async () => {
    const userId = signal('user-123');
    const body = signal<{ email: string } | null>(null);

    const resource = TestBed.runInInjectionContext(() =>
      service.updateUserProfile(userId, body as Parameters<typeof service.updateUserProfile>[1]),
    );

    await flushEffects();

    // When body is null, no request fires — the httpResource factory returns undefined
    httpMock.expectNone(`${TEST_API_URL}/user/user-123`);

    // After setting body, the request should fire
    body.set({ email: 'new@example.com' });

    await flushEffects();

    const req = httpMock.expectOne(`${TEST_API_URL}/user/user-123`);
    expect(req.request.method).toBe('PATCH');
    expect(req.request.body).toEqual({ email: 'new@example.com' });

    req.flush({
      id: 'user-123',
      username: 'testuser',
      email: 'new@example.com',
      favoriteBlogPostIds: [],
      favoriteProjectIds: [],
      createdBlogPostIds: [],
    });

    await flushEffects();

    expect(resource.value()?.email).toBe('new@example.com');
  });
});
