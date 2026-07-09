import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';

import { WebConfigService } from '@web/shared';
import type { CreateBlogPost } from '@shared/types';

import { BlogPostService } from './blog-post.service';

const TEST_API_URL = '/api/v1';

describe('BlogPostService', () => {
  let service: BlogPostService;
  let httpMock: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [BlogPostService, WebConfigService, provideHttpClient(), provideHttpClientTesting()],
    });

    const webConfig = TestBed.inject(WebConfigService);
    webConfig.API_URL = TEST_API_URL;

    service = TestBed.inject(BlogPostService);
    httpMock = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    httpMock.verify();
  });

  describe('getBlogPosts', () => {
    it('sends GET to the correct URL with pagesize and page params', async () => {
      const postsPerPage = signal(10);
      const currentPage = signal(3); // backend uses 0-based index: page=2

      const resource = TestBed.runInInjectionContext(() => service.getBlogPosts(postsPerPage, currentPage));

      await TestBed.flushEffects();

      const req = httpMock.expectOne(`${TEST_API_URL}/blog-post?pagesize=10&page=2`);
      expect(req.request.method).toBe('GET');

      req.flush({ posts: [], count: 0 });

      await TestBed.flushEffects();

      expect(resource.hasValue()).toBe(true);
      expect(resource.value()).toEqual({ posts: [], count: 0 });
    });
  });

  describe('getBlogPostById', () => {
    it('sends GET to the correct URL with the post id', async () => {
      const id = signal('post-abc');

      const resource = TestBed.runInInjectionContext(() => service.getBlogPostById(id));

      await TestBed.flushEffects();

      const req = httpMock.expectOne(`${TEST_API_URL}/blog-post/post-abc`);
      expect(req.request.method).toBe('GET');

      req.flush({
        _id: 'post-abc',
        title: 'Test Post',
        subtitle: '...',
        description: '...',
        thumbnail: '...',
        dateTimeCreated: new Date(),
      });

      await TestBed.flushEffects();

      expect(resource.hasValue()).toBe(true);
      expect(resource.value()?._id).toBe('post-abc');
    });
  });

  describe('createBlogPost', () => {
    it('does not fire an HTTP request when the signal is null', () => {
      const post = signal<CreateBlogPost | null>(null);

      TestBed.runInInjectionContext(() => service.createBlogPost(post));

      // The tracking function returns undefined → no fetch is performed
      httpMock.expectNone(`${TEST_API_URL}/blog-post`);
    });

    it('sends POST when the signal is set to a non-null value', async () => {
      const post = signal<CreateBlogPost | null>(null);
      const resource = TestBed.runInInjectionContext(() => service.createBlogPost(post));

      await TestBed.flushEffects();

      const body: CreateBlogPost = {
        title: 'New Post',
        subtitle: 'Subtitle',
        description: 'Description',
        thumbnail: 'thumb.png',
        dateTimeCreated: new Date(),
      };

      post.set(body);

      await TestBed.flushEffects();

      const req = httpMock.expectOne(`${TEST_API_URL}/blog-post`);
      expect(req.request.method).toBe('POST');
      expect(req.request.body).toEqual(body);

      req.flush({ ...body, _id: 'post-new' });

      await TestBed.flushEffects();

      expect(resource.hasValue()).toBe(true);
      expect(resource.value()?._id).toBe('post-new');
    });
  });

  describe('deleteBlogPost', () => {
    it('does not fire an HTTP request when the signal is null', () => {
      const id = signal<string | null>(null);

      TestBed.runInInjectionContext(() => service.deleteBlogPost(id));

      httpMock.expectNone(`${TEST_API_URL}/blog-post`);
    });

    it('sends DELETE when the signal is set to a non-null value', async () => {
      const id = signal<string | null>(null);
      const resource = TestBed.runInInjectionContext(() => service.deleteBlogPost(id));

      await TestBed.flushEffects();

      id.set('post-to-delete');

      await TestBed.flushEffects();

      const req = httpMock.expectOne(`${TEST_API_URL}/blog-post/post-to-delete`);
      expect(req.request.method).toBe('DELETE');

      req.flush({});

      await TestBed.flushEffects();

      expect(resource.hasValue()).toBe(true);
    });
  });
});
