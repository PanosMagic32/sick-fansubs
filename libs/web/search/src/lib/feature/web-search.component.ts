import { ChangeDetectionStrategy, Component, computed, inject, OnDestroy, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { FormControl, FormGroup, ReactiveFormsModule, Validators } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { filter, map, switchMap, tap } from 'rxjs';

import { MatButton } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { MatIcon } from '@angular/material/icon';
import { MatPaginator, type PageEvent } from '@angular/material/paginator';
import { MatProgressBar } from '@angular/material/progress-bar';

import { NoContentComponent, TokenService } from '@web/shared';

import { SearchFormModel, SearchTypeOption } from '../data-access/search-form.interface';
import { SearchService } from '../data-access/search.service';
import { WebSearchResultItemComponent } from '../ui/web-search-result-item.component';

const DEFAULT_PAGE = 1;
const DEFAULT_PAGE_SIZE = 5;
const PAGE_SIZE_OPTIONS = [5, 10, 20, 50, 100];

@Component({
  selector: 'sf-web-search',
  templateUrl: './web-search.component.html',
  styleUrl: './web-search.component.scss',
  imports: [
    ReactiveFormsModule,
    MatFormFieldModule,
    MatInputModule,
    MatSelectModule,
    MatButton,
    MatIcon,
    MatPaginator,
    NoContentComponent,
    MatProgressBar,
    WebSearchResultItemComponent,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class WebSearchComponent implements OnDestroy {
  private readonly activatedRoute = inject(ActivatedRoute);
  private readonly router = inject(Router);
  private readonly searchService = inject(SearchService);
  private readonly tokenService = inject(TokenService);

  protected readonly hasSubmitted = signal(false);
  protected readonly searchTerm = signal<string>('');
  protected readonly currentPage = signal(DEFAULT_PAGE);
  protected readonly pageSize = signal(DEFAULT_PAGE_SIZE);
  protected readonly pageSizeOptions = signal(PAGE_SIZE_OPTIONS);
  protected readonly searchType = signal<SearchTypeOption>('all');

  protected readonly isAdmin = this.tokenService.isAdmin;
  protected readonly isLoading = this.searchService.isLoading;
  protected readonly results = this.searchService.results;

  protected readonly totalResults = computed(() => this.results().total);

  protected readonly searchForm = new FormGroup<SearchFormModel>({
    searchTerm: new FormControl('', {
      nonNullable: true,
      validators: [Validators.required, Validators.minLength(3), Validators.maxLength(100)],
    }),
    type: new FormControl('all', { nonNullable: true }),
  });

  get formControls() {
    return this.searchForm.controls;
  }

  constructor() {
    this.activatedRoute.queryParamMap
      .pipe(
        takeUntilDestroyed(),
        filter((params) => params.has('searchTerm') || params.has('page') || params.has('pageSize')),
        map((params) => ({
          searchTerm: params.get('searchTerm')?.trim()?.toLowerCase() ?? this.searchTerm(),
          searchType: (params.get('type') as SearchTypeOption) ?? this.searchType(),
          page: +(params.get('page') ?? this.currentPage()),
          pageSize: +(params.get('pageSize') ?? this.pageSize()),
        })),
        tap(({ searchTerm, searchType, page, pageSize }) => {
          this.searchTerm.set(searchTerm);
          this.searchType.set(searchType);
          this.currentPage.set(page);
          this.pageSize.set(pageSize);
        }),
        switchMap(({ searchTerm, searchType, page, pageSize }) =>
          this.searchService.search(searchTerm, searchType, pageSize, page - 1),
        ),
      )
      .subscribe();
  }

  ngOnDestroy() {
    // TODO: temp cleanup
    this.activatedRoute.snapshot.queryParams = {};
    this.activatedRoute.snapshot.params = {};
    this.searchService.results().results = [];
    this.searchService.results().total = 0;
  }

  onSubmit() {
    if (this.searchForm.invalid) return;
    this.hasSubmitted.set(true);
    this.handleQueryParamsNavigation({
      page: DEFAULT_PAGE,
      searchTerm: this.searchForm.value.searchTerm,
      type: this.searchForm.value.type,
    });
  }

  onPageChange(event: PageEvent) {
    this.handleQueryParamsNavigation({ page: event.pageIndex + 1, pageSize: event.pageSize });
  }

  private handleQueryParamsNavigation(
    params: Partial<{ searchTerm: string; type: SearchTypeOption; page: number; pageSize: number }>,
  ) {
    this.router.navigate([], {
      queryParams: { ...params },
      queryParamsHandling: 'merge',
      relativeTo: this.activatedRoute,
    });
  }
}
