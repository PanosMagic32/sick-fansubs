import { HttpClient } from '@angular/common/http';
import { Injectable } from '@angular/core';
import { BehaviorSubject, Observable, Subject } from 'rxjs';

import { MatSnackBar } from '@angular/material/snack-bar';

import { Project } from '@sick/api/project';
import { ConfigService } from '@sick/shared';

@Injectable({
  providedIn: 'root',
})
export class ProjectsService {
  isLoading$ = new BehaviorSubject(false);

  private projects: Project[] = [];
  private projectsUpdated = new Subject<{ projects: Project[]; count: number }>();

  constructor(private http: HttpClient, private snackBar: MatSnackBar, private configService: ConfigService) {}

  getProjects(projectsPerPage: number, currentPage: number) {
    this.isLoading$.next(true);

    this.http
      .get<{ projects: Project[]; count: number }>(
        `${this.configService.API_URL}/project?pagesize=${projectsPerPage}&page=${currentPage}`
      )
      .subscribe({
        next: (res) => {
          this.projects = res.projects;
          this.projectsUpdated.next({
            projects: [...this.projects],
            count: res.count,
          });

          this.isLoading$.next(false);
        },
        error: (err) => {
          this.openSnackBar(err.status === 0 ? 'Uknown error.' : err.error.message, 'OK');
          this.isLoading$.next(false);
        },
      });
  }

  getProjectById(id: string): Observable<Project> {
    return this.http.get<Project>(`${this.configService.API_URL}/project/${id}`);
  }

  getProjectUpdateListener() {
    return this.projectsUpdated.asObservable();
  }

  private openSnackBar(message: string, action: string) {
    this.snackBar.open(message, action, { duration: 3000 });
  }
}
