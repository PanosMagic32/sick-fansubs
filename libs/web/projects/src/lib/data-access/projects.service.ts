import { httpResource, HttpResourceRef } from '@angular/common/http';
import { inject, Injectable, Signal, WritableSignal } from '@angular/core';

import { WebConfigService } from '@web/shared';

import type { Project, ProjectBatchDownloadLink, ProjectResponse } from './project.interface';

const HTTP_URL_PATTERN = /^https?:\/\//i;
const MAGNET_URL_PATTERN = /^magnet:\?xt=/i;

type UnknownRecord = Record<string, unknown>;

@Injectable({ providedIn: 'root' })
export class ProjectsService {
  private readonly webConfigService = inject(WebConfigService);

  private isRecord(value: unknown): value is UnknownRecord {
    return value !== null && typeof value === 'object' && !Array.isArray(value);
  }

  private asProjectArray(value: unknown): unknown[] {
    if (Array.isArray(value)) return value;

    if (!this.isRecord(value)) return [];

    const numericEntries = Object.entries(value)
      .filter(([key]) => /^\d+$/.test(key))
      .sort((a, b) => Number(a[0]) - Number(b[0]))
      .map(([, entryValue]) => entryValue);

    if (numericEntries.length > 0) return numericEntries;

    return [];
  }

  private toStringValue(value: unknown): string {
    if (typeof value === 'string') return value.trim();

    if (!this.isRecord(value)) return '';

    const oid = value['$oid'];
    if (typeof oid === 'string') return oid.trim();

    const date = value['$date'];
    if (typeof date === 'string') return date.trim();

    return '';
  }

  private toDateValue(value: unknown): string {
    if (typeof value === 'string' && !Number.isNaN(new Date(value).getTime())) {
      return value;
    }

    return new Date().toISOString();
  }

  private toOptionalDateValue(value: unknown): string | undefined {
    if (typeof value === 'string' && !Number.isNaN(new Date(value).getTime())) {
      return value;
    }

    return undefined;
  }

  private extractIndexedString(value: unknown): string {
    if (!this.isRecord(value)) return '';

    const orderedCharEntries = Object.entries(value)
      .filter(([key, char]) => /^\d+$/.test(key) && typeof char === 'string')
      .sort((a, b) => Number(a[0]) - Number(b[0]))
      .map(([, char]) => char);

    return orderedCharEntries.join('').trim();
  }

  private normalizeBatchDownloadLink(link: unknown, index: number): ProjectBatchDownloadLink | null {
    if (typeof link === 'string') {
      const trimmed = link.trim();

      return {
        name: `Batch ${index + 1}`,
        downloadLinkTorrent: HTTP_URL_PATTERN.test(trimmed) ? trimmed : '',
        downloadLink: MAGNET_URL_PATTERN.test(trimmed) ? trimmed : '',
        downloadLink4kTorrent: undefined,
        downloadLink4k: undefined,
      };
    }

    if (!this.isRecord(link)) {
      return null;
    }

    const indexedValue = this.extractIndexedString(link);
    const legacyUrlValue = this.toStringValue(link['url']);

    const torrentValue = this.toStringValue(link['downloadLinkTorrent']);
    const magnetValue = this.toStringValue(link['downloadLink']);
    const torrent4kValue = this.toStringValue(link['downloadLink4kTorrent']);
    const magnet4kValue = this.toStringValue(link['downloadLink4k']);

    const resolvedTorrent = torrentValue || (HTTP_URL_PATTERN.test(legacyUrlValue) ? legacyUrlValue : '');
    const resolvedMagnet = magnetValue || (MAGNET_URL_PATTERN.test(legacyUrlValue) ? legacyUrlValue : '');

    const indexedTorrent = HTTP_URL_PATTERN.test(indexedValue) ? indexedValue : '';
    const indexedMagnet = MAGNET_URL_PATTERN.test(indexedValue) ? indexedValue : '';

    return {
      name: this.toStringValue(link['name']) || `Batch ${index + 1}`,
      downloadLinkTorrent: resolvedTorrent || indexedTorrent,
      downloadLink: resolvedMagnet || indexedMagnet,
      downloadLink4kTorrent: torrent4kValue || undefined,
      downloadLink4k: magnet4kValue || undefined,
    };
  }

  private normalizeProject(raw: unknown, fallbackIndex = 0): Project {
    const project = this.isRecord(raw) ? raw : {};

    const rawBatchDownloadLinks = Array.isArray(project['batchDownloadLinks'])
      ? project['batchDownloadLinks']
      : this.asProjectArray(project['batchDownloadLinks']);

    const normalizedBatchDownloadLinks = rawBatchDownloadLinks
      .map((link, index) => this.normalizeBatchDownloadLink(link, index))
      .filter(
        (link): link is ProjectBatchDownloadLink =>
          !!link && (link.downloadLinkTorrent.length > 0 || link.downloadLink.length > 0),
      );

    return {
      _id: this.toStringValue(project['_id']),
      title: this.toStringValue(project['title']) || `Project ${fallbackIndex + 1}`,
      description: this.toStringValue(project['description']),
      thumbnail: this.toStringValue(project['thumbnail']),
      dateTimeCreated: this.toDateValue(project['dateTimeCreated']),
      creator: this.isRecord(project['creator']) ? (project['creator'] as Project['creator']) : undefined,
      updatedBy: this.isRecord(project['updatedBy']) ? (project['updatedBy'] as Project['updatedBy']) : undefined,
      updatedAt: this.toOptionalDateValue(project['updatedAt']),
      batchDownloadLinks: normalizedBatchDownloadLinks,
    };
  }

  private normalizeProjectResponse(raw: unknown): ProjectResponse {
    if (!this.isRecord(raw)) {
      return { projects: [], count: 0 };
    }

    const rawProjectsSource = raw['projects'] ?? raw;

    let rawProjects: unknown[];
    if (Array.isArray(rawProjectsSource)) {
      rawProjects = rawProjectsSource;
    } else if (this.isRecord(rawProjectsSource) && typeof rawProjectsSource['title'] === 'string') {
      rawProjects = [rawProjectsSource];
    } else {
      rawProjects = this.asProjectArray(rawProjectsSource);
    }

    const projects = rawProjects.map((project, index) => this.normalizeProject(project, index));

    const count = typeof raw['count'] === 'number' ? raw['count'] : projects.length;

    return { projects, count };
  }

  private normalizeProjectEntity(raw: unknown): Project {
    if (this.isRecord(raw) && typeof raw['title'] === 'string') {
      return this.normalizeProject(raw);
    }

    const legacyProjects = this.asProjectArray(raw);
    return this.normalizeProject(legacyProjects[0]);
  }

  createProject(project: WritableSignal<Project | null>): HttpResourceRef<Project | undefined> {
    return httpResource<Project>(
      () => {
        const body = project();
        if (!body) return;

        return {
          url: `${this.webConfigService.API_URL}/project`,
          method: 'POST',
          body,
        };
      },
      {
        parse: (value) => this.normalizeProjectEntity(value),
      },
    );
  }

  getProjects(projectsPerPage: Signal<number>, currentPage: Signal<number>): HttpResourceRef<ProjectResponse | undefined> {
    return httpResource<ProjectResponse>(
      () => ({
        url: `${this.webConfigService.API_URL}/project?pagesize=${projectsPerPage()}&page=${currentPage() - 1}`,
      }),
      {
        parse: (value) => this.normalizeProjectResponse(value),
      },
    );
  }

  getProjectById(id: Signal<string>): HttpResourceRef<Project | undefined> {
    return httpResource<Project>(
      () => ({
        url: `${this.webConfigService.API_URL}/project/${id()}`,
      }),
      {
        parse: (value) => this.normalizeProjectEntity(value),
      },
    );
  }

  updateProject(id: Signal<string>, updateProject: WritableSignal<Project | null>): HttpResourceRef<Project | undefined> {
    return httpResource<Project>(
      () => {
        const body = updateProject();
        if (!body) return;

        return {
          url: `${this.webConfigService.API_URL}/project/${id()}`,
          method: 'PATCH',
          body,
        };
      },
      {
        parse: (value) => this.normalizeProjectEntity(value),
      },
    );
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
