import { HttpClient } from '@angular/common/http';
import { Injectable } from '@angular/core';
import { Router } from '@angular/router';
import { BehaviorSubject, Observable } from 'rxjs';

import { MatSnackBar } from '@angular/material/snack-bar';

import { ConfigService } from '@sick/shared';

import { Project, ProjectResponse } from './project.interface';

@Injectable({
  providedIn: 'root',
})
export class ProjectsService {
  private isLoading = new BehaviorSubject(false);
  private projects = new BehaviorSubject<ProjectResponse>({ projects: [], count: 0 });

  get isLoading$() {
    return this.isLoading.asObservable();
  }

  get projects$() {
    return this.projects.asObservable();
  }

  constructor(
    private readonly http: HttpClient,
    private readonly snackBar: MatSnackBar,
    private readonly configService: ConfigService,
    private readonly router: Router,
  ) {}

  getProjects(projectsPerPage: number, currentPage: number) {
    this.isLoading.next(true);

    return this.http
      .get<ProjectResponse>(`${this.configService.API_URL}/project?pagesize=${projectsPerPage}&page=${currentPage}`)
      .subscribe({
        next: (res) => {
          this.projects.next({
            projects: [...res.projects],
            count: res.count,
          });

          this.isLoading.next(false);
        },
        error: (err) => {
          this.openSnackBar(err.status === 0 ? 'Uknown error.' : err.error.message, 'OK');
          this.isLoading.next(false);
        },
      });
  }

  getProjectById(id: string): Observable<Project> {
    return this.http.get<Project>(`${this.configService.API_URL}/project/${id}`);
  }

  updateProject(id: string, updateProject: Project) {
    this.isLoading.next(true);

    this.http.patch<Project>(`${this.configService.API_URL}/project/${id}`, updateProject).subscribe({
      next: () => {
        this.isLoading.next(false);
        this.router.navigate(['/projects'], { replaceUrl: true });
      },
      error: (err) => {
        this.openSnackBar(err.status === 0 ? 'Uknown error.' : err.error.message, 'OK');
        this.isLoading.next(false);
      },
    });
  }

  deleteProject(id: string) {
    return this.http.delete(`${this.configService.API_URL}/projects/${id}`);
  }

  private openSnackBar(message: string, action: string) {
    this.snackBar.open(message, action, { duration: 3000 });
  }
}
