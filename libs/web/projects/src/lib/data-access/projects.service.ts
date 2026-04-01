import { httpResource, HttpResourceRef } from '@angular/common/http';
import { inject, Injectable, Signal, WritableSignal } from '@angular/core';

import { WebConfigService } from '@web/shared';

import type { Project, ProjectResponse } from './project.interface';

@Injectable({ providedIn: 'root' })
export class ProjectsService {
  private readonly webConfigService = inject(WebConfigService);

  createProject(project: WritableSignal<Project | null>): HttpResourceRef<Project | undefined> {
    return httpResource<Project>(() => {
      const body = project();
      if (!body) return;

      return {
        url: `${this.webConfigService.API_URL}/project`,
        method: 'POST',
        body,
      };
    });
  }

  getProjects(projectsPerPage: Signal<number>, currentPage: Signal<number>): HttpResourceRef<ProjectResponse | undefined> {
    return httpResource<ProjectResponse>(() => ({
      url: `${this.webConfigService.API_URL}/project?pagesize=${projectsPerPage()}&page=${currentPage() - 1}`,
    }));
  }

  getProjectById(id: Signal<string>): HttpResourceRef<Project | undefined> {
    return httpResource<Project>(() => ({
      url: `${this.webConfigService.API_URL}/project/${id()}`,
    }));
  }

  updateProject(id: Signal<string>, updateProject: WritableSignal<Project | null>): HttpResourceRef<Project | undefined> {
    return httpResource<Project>(() => {
      const body = updateProject();
      if (!body) return;

      return {
        url: `${this.webConfigService.API_URL}/project/${id()}`,
        method: 'PATCH',
        body,
      };
    });
  }

  deleteProject(id: WritableSignal<string | null>): HttpResourceRef<Project | undefined> {
    return httpResource<Project>(() => {
      const projectId = id();
      if (!projectId) return;

      return {
        url: `${this.webConfigService.API_URL}/project/${projectId}`,
        method: 'DELETE',
      };
    });
  }
}
