import { ApplicationRef, EnvironmentInjector, signal } from '@angular/core';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';

import { WebConfigService } from '@web/shared';

import { ProjectsService } from './projects.service';

const TEST_API_URL = '/api/v1';

describe('ProjectsService', () => {
  let service: ProjectsService;
  let httpMock: HttpTestingController;
  let injector: EnvironmentInjector;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [ProjectsService, WebConfigService, provideHttpClient(), provideHttpClientTesting()],
    });

    const webConfig = TestBed.inject(WebConfigService);
    webConfig.API_URL = TEST_API_URL;

    service = TestBed.inject(ProjectsService);
    httpMock = TestBed.inject(HttpTestingController);
    injector = TestBed.inject(EnvironmentInjector);
  });

  afterEach(() => {
    httpMock.verify();
  });

  describe('getProjects', () => {
    it('calls the correct URL with page size and page params', () => {
      const pageSize = signal(10);
      const currentPage = signal(2);

      injector.runInContext(() => service.getProjects(pageSize, currentPage));

      // Trigger change detection so the httpResource fires its request
      TestBed.inject(ApplicationRef).tick();

      const req = httpMock.expectOne(`${TEST_API_URL}/project?pagesize=10&page=1`);
      expect(req.request.method).toBe('GET');
      req.flush({ projects: [], count: 0 });
    });

    it('updates URL when signals change', () => {
      const pageSize = signal(10);
      const currentPage = signal(1);

      injector.runInContext(() => service.getProjects(pageSize, currentPage));
      TestBed.inject(ApplicationRef).tick();

      const req1 = httpMock.expectOne(`${TEST_API_URL}/project?pagesize=10&page=0`);
      req1.flush({ projects: [], count: 0 });

      pageSize.set(20);
      currentPage.set(3);
      TestBed.inject(ApplicationRef).tick();

      const req2 = httpMock.expectOne(`${TEST_API_URL}/project?pagesize=20&page=2`);
      expect(req2.request.method).toBe('GET');
      req2.flush({ projects: [], count: 0 });
    });
  });

  describe('getProjectById', () => {
    it('calls the correct URL with the project ID', () => {
      const id = signal('abc123');

      injector.runInContext(() => service.getProjectById(id));
      TestBed.inject(ApplicationRef).tick();

      const req = httpMock.expectOne(`${TEST_API_URL}/project/abc123`);
      expect(req.request.method).toBe('GET');
      req.flush({ _id: 'abc123', title: 'Test Project' });
    });
  });

  describe('createProject', () => {
    it('does not fire a request when signal is null', () => {
      const projectSignal = signal<Record<string, unknown> | null>(null);

      injector.runInContext(() => service.createProject(projectSignal));
      TestBed.inject(ApplicationRef).tick();

      httpMock.expectNone(`${TEST_API_URL}/project`);
    });

    it('fires a POST request when signal is non-null', () => {
      const projectBody = { title: 'New Project', description: 'A test' };
      const projectSignal = signal<Record<string, unknown> | null>(projectBody);

      injector.runInContext(() => service.createProject(projectSignal));
      TestBed.inject(ApplicationRef).tick();

      const req = httpMock.expectOne(`${TEST_API_URL}/project`);
      expect(req.request.method).toBe('POST');
      expect(req.request.body).toEqual(projectBody);
      req.flush({ _id: 'new-id', title: 'New Project' });
    });
  });
});
