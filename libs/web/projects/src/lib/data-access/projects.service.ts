import { HttpClient } from '@angular/common/http';
import { inject, Injectable, signal } from '@angular/core';
import { Router } from '@angular/router';
import { catchError, tap } from 'rxjs';

import { MatSnackBar } from '@angular/material/snack-bar';

import { WebConfigService } from '@web/shared';

import type { Project, ProjectResponse } from './project.interface';

@Injectable({ providedIn: 'root' })
export class ProjectsService {
  private readonly httpClient = inject(HttpClient);
  private readonly snackBar = inject(MatSnackBar);
  private readonly webConfigService = inject(WebConfigService);
  private readonly router = inject(Router);

  private _isLoading = signal(false);
  isLoading = this._isLoading.asReadonly();

  private _projects = signal<ProjectResponse>({ projects: [], count: 0 });
  projects = this._projects.asReadonly();

  private _selectedProject = signal<Project | null>(null);
  selectedProject = this._selectedProject.asReadonly();

  createProject(project: Project) {
    this.httpClient.post(`${this.webConfigService.API_URL}/project`, project).subscribe({
      next: () => {
        this._isLoading.set(false);
        this.router.navigate(['/', 'projects'], { replaceUrl: true });
      },
      error: (err) => {
        this.openSnackBar(err.status === 0 ? 'Uknown error.' : err.error.message, 'OK');
        this._isLoading.set(false);
      },
    });
  }

  getProjects(projectsPerPage: number, currentPage: number) {
    this._isLoading.set(true);

    return this.httpClient
      .get<ProjectResponse>(`${this.webConfigService.API_URL}/project?pagesize=${projectsPerPage}&page=${currentPage}`)
      .pipe(
        catchError((err) => {
          this.openSnackBar(err.status === 0 ? 'Uknown error.' : err.error.message, 'OK');
          this._isLoading.set(false);
          return [];
        }),
        tap((res) => {
          this._projects.set({
            projects: [...res.projects],
            count: res.count,
          });

          this._isLoading.set(false);
        }),
      );
  }

  getProjectById(id: string) {
    this.httpClient
      .get<Project>(`${this.webConfigService.API_URL}/project/${id}`)
      .subscribe((project) => this._selectedProject.set(project));
  }

  updateProject(id: string, updateProject: Project) {
    this._isLoading.set(true);

    this.httpClient.patch<Project>(`${this.webConfigService.API_URL}/project/${id}`, updateProject).subscribe({
      next: () => {
        this._isLoading.set(false);
        this.router.navigate(['/', 'projects'], { replaceUrl: true });
      },
      error: (err) => {
        this.openSnackBar(err.status === 0 ? 'Uknown error.' : err.error.message, 'OK');
        this._isLoading.set(false);
      },
    });
  }

  deleteProject(id: string) {
    this.httpClient.delete(`${this.webConfigService.API_URL}/project/${id}`).subscribe({
      next: () => {
        this._isLoading.set(false);
        this.router.navigate(['/', 'projects'], { replaceUrl: true });
      },
      error: (err) => {
        this.openSnackBar(err.status === 0 ? 'Uknown error.' : err.error.message, 'OK');
        this._isLoading.set(false);
      },
    });
  }

  private openSnackBar(message: string, action: string) {
    this.snackBar.open(message, action, { duration: 3000 });
  }
}
