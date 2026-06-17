import { HttpClient } from '@angular/common/http';
import { inject, Injectable, signal } from '@angular/core';

import { MatSnackBar } from '@angular/material/snack-bar';

import { mapApiErrorMessage, WebConfigService } from '@web/shared';

import { SearchResponse } from './search-form.interface';
import { catchError, of, tap } from 'rxjs';

@Injectable({ providedIn: 'root' })
export class SearchService {
  private readonly httpClient = inject(HttpClient);
  private readonly snackBar = inject(MatSnackBar);
  private readonly webConfigService = inject(WebConfigService);

  private _isLoading = signal(false);
  isLoading = this._isLoading.asReadonly();

  private _results = signal<SearchResponse>({ results: [], total: 0 });
  results = this._results.asReadonly();

  clearResults() {
    this._results.set({ results: [], total: 0 });
    this._isLoading.set(false);
  }

  search(searchTerm: string, searchType: 'all' | 'blog-post' | 'project', pageSize: number, currentPage: number) {
    this._isLoading.set(true);

    return this.httpClient
      .get<SearchResponse>(
        `${this.webConfigService.API_URL}/search?searchTerm=${searchTerm}&type=${searchType}&pageSize=${pageSize}&page=${currentPage}`,
      )
      .pipe(
        catchError((err) => {
          this.openSnackBar(mapApiErrorMessage(err), 'OK');
          this._isLoading.set(false);
          return of({ results: [], total: 0 });
        }),
        tap((res) => {
          this._results.set({
            results: [...res.results],
            total: res.total,
          });

          this._isLoading.set(false);
        }),
      );
  }

  private openSnackBar(message: string, action: string) {
    this.snackBar.open(message, action, { duration: 3000 });
  }
}
