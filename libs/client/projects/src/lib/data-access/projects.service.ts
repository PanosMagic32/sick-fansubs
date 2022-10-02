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

  private projects = new Subject<Project[]>();

  constructor(private http: HttpClient, private snackBar: MatSnackBar, private configService: ConfigService) {}

  getProjects() {
    this.isLoading$.next(true);

    this.http.get<Project[]>(`${this.configService.API_URL}/project`).subscribe({
      next: (res) => {
        this.projects.next(res);
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
    return this.projects.asObservable();
  }

  private openSnackBar(message: string, action: string) {
    this.snackBar.open(message, action, { duration: 3000 });
  }
}
