import { HttpClient } from '@angular/common/http';
import { Injectable } from '@angular/core';
import { BehaviorSubject, Observable } from 'rxjs';

import { MatSnackBar } from '@angular/material/snack-bar';

import { Project } from '@sick/api/project';
import { ConfigService } from '@sick/shared';

import { ProjectResponse } from './project.interface';

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

  constructor(private http: HttpClient, private snackBar: MatSnackBar, private configService: ConfigService) {}

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

  private openSnackBar(message: string, action: string) {
    this.snackBar.open(message, action, { duration: 3000 });
  }
}
