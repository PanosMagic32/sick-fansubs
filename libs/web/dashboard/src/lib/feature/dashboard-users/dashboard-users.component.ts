import { DatePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { toObservable, toSignal } from '@angular/core/rxjs-interop';
import { debounceTime, distinctUntilChanged, map } from 'rxjs';

import { MatCard, MatCardContent, MatCardHeader, MatCardTitle } from '@angular/material/card';
import { MatChip, MatChipSet } from '@angular/material/chips';
import { MatFormField, MatLabel } from '@angular/material/form-field';
import { MatIcon } from '@angular/material/icon';
import { MatInput } from '@angular/material/input';
import { MatPaginatorModule, PageEvent } from '@angular/material/paginator';
import { MatOption, MatSelect, MatSelectChange } from '@angular/material/select';
import { MatSortModule, Sort } from '@angular/material/sort';
import { MatTableModule } from '@angular/material/table';

import type { UserRole, UserStatus } from '@shared/types';

import { DashboardService } from '../../data-access/dashboard.service';

@Component({
  selector: 'sf-dashboard-users',
  templateUrl: './dashboard-users.component.html',
  styleUrl: './dashboard-users.component.scss',
  imports: [
    DatePipe,
    MatCard,
    MatCardHeader,
    MatCardTitle,
    MatCardContent,
    MatChipSet,
    MatChip,
    MatFormField,
    MatLabel,
    MatInput,
    MatIcon,
    MatSelect,
    MatOption,
    MatTableModule,
    MatSortModule,
    MatPaginatorModule,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class DashboardUsersComponent {
  private readonly dashboardService = inject(DashboardService);
  private readonly defaultAvatarPath = '/logo/logo.png';

  protected readonly searchText = signal('');
  protected readonly debouncedSearchText = toSignal(
    toObservable(this.searchText).pipe(
      map((value) => value.trim()),
      debounceTime(300),
      distinctUntilChanged(),
    ),
    { initialValue: '' },
  );
  protected readonly currentPage = signal(1);
  protected readonly pageSize = signal(10);
  protected readonly roleFilter = signal<'all' | UserRole>('all');
  protected readonly statusFilter = signal<'all' | UserStatus>('all');
  protected readonly sortBy = signal<'username' | 'email' | 'role' | 'status' | 'createdAt' | 'updatedAt'>('updatedAt');
  protected readonly sortDirection = signal<'asc' | 'desc'>('desc');

  protected readonly usersResource = this.dashboardService.getManagementUsers({
    page: this.currentPage,
    pageSize: this.pageSize,
    search: this.debouncedSearchText,
    role: this.roleFilter,
    status: this.statusFilter,
    sortBy: this.sortBy,
    sortDirection: this.sortDirection,
  });

  protected readonly users = computed(() => this.usersResource.value()?.users ?? []);
  protected readonly displayedColumns: ReadonlyArray<string> = ['username', 'email', 'role', 'status', 'updatedAt'];
  protected readonly pageSizeOptions: ReadonlyArray<number> = [10, 20, 50];

  protected readonly totalUsersCount = computed(() => this.usersResource.value()?.count ?? 0);

  protected readonly activeUsersCount = computed(() => this.users().filter((user) => user.status === 'active').length);

  protected readonly suspendedUsersCount = computed(() => this.users().filter((user) => user.status === 'suspended').length);

  protected readonly hasError = computed(() => Boolean(this.usersResource.error()));

  protected readonly roleOptions: ReadonlyArray<{ value: 'all' | UserRole; label: string }> = [
    { value: 'all', label: 'Όλοι οι ρόλοι' },
    { value: 'super-admin', label: 'Υπερδιαχειριστής' },
    { value: 'admin', label: 'Διαχειριστής' },
    { value: 'moderator', label: 'Συντονιστής' },
    { value: 'user', label: 'Χρήστης' },
  ];

  protected readonly statusOptions: ReadonlyArray<{ value: 'all' | UserStatus; label: string }> = [
    { value: 'all', label: 'Όλες οι καταστάσεις' },
    { value: 'active', label: 'Ενεργός' },
    { value: 'suspended', label: 'Σε αναστολή' },
  ];

  protected onSearchChange(value: string): void {
    this.searchText.set(value ?? '');
    this.currentPage.set(1);
  }

  protected onRoleFilterChange(event: MatSelectChange): void {
    this.roleFilter.set(event.value as 'all' | UserRole);
    this.currentPage.set(1);
  }

  protected onStatusFilterChange(event: MatSelectChange): void {
    this.statusFilter.set(event.value as 'all' | UserStatus);
    this.currentPage.set(1);
  }

  protected onSortChange(event: Sort): void {
    const direction = event.direction || 'desc';
    const active = (event.active || 'updatedAt') as 'username' | 'email' | 'role' | 'status' | 'createdAt' | 'updatedAt';

    this.sortBy.set(active);
    this.sortDirection.set(direction as 'asc' | 'desc');
    this.currentPage.set(1);
  }

  protected onPageChange(event: PageEvent): void {
    this.currentPage.set(event.pageIndex + 1);
    this.pageSize.set(event.pageSize);
  }

  protected roleLabel(role: UserRole): string {
    switch (role) {
      case 'super-admin':
        return 'Υπερδιαχειριστής';
      case 'admin':
        return 'Διαχειριστής';
      case 'moderator':
        return 'Συντονιστής';
      default:
        return 'Χρήστης';
    }
  }

  protected statusLabel(status: UserStatus): string {
    return status === 'active' ? 'Ενεργός' : 'Σε αναστολή';
  }

  protected avatarUrl(avatar?: string): string {
    return avatar?.trim() || this.defaultAvatarPath;
  }

  protected onAvatarImageError(event: Event): void {
    const imageElement = event.target as HTMLImageElement | null;
    if (!imageElement || imageElement.src.endsWith(this.defaultAvatarPath)) {
      return;
    }

    imageElement.src = this.defaultAvatarPath;
  }
}
