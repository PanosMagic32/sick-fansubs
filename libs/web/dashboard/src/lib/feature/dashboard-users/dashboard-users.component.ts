import { DatePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, signal, viewChild } from '@angular/core';
import { toObservable, toSignal } from '@angular/core/rxjs-interop';
import { debounceTime, distinctUntilChanged, firstValueFrom, map } from 'rxjs';

import { MatButtonModule, MatIconButton } from '@angular/material/button';
import { MatCard, MatCardContent, MatCardHeader, MatCardTitle } from '@angular/material/card';
import { MatCheckbox, MatCheckboxChange } from '@angular/material/checkbox';
import { MatChip, MatChipSet } from '@angular/material/chips';
import { MatDialog } from '@angular/material/dialog';
import { MatFormField, MatLabel } from '@angular/material/form-field';
import { MatIcon } from '@angular/material/icon';
import { MatInput } from '@angular/material/input';
import { MatMenuModule } from '@angular/material/menu';
import { MatPaginatorModule, PageEvent } from '@angular/material/paginator';
import { MatOption, MatSelect, MatSelectChange } from '@angular/material/select';
import { MatSortModule, Sort } from '@angular/material/sort';
import { MatSnackBar } from '@angular/material/snack-bar';
import { MatTableModule } from '@angular/material/table';

import type { UserRole, UserStatus } from '@shared/types';
import { ConfirmDialogComponent, TokenService } from '@web/shared';

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
    MatButtonModule,
    MatIconButton,
    MatCheckbox,
    MatChipSet,
    MatChip,
    MatFormField,
    MatLabel,
    MatInput,
    MatIcon,
    MatMenuModule,
    MatSelect,
    MatOption,
    MatTableModule,
    MatSortModule,
    MatPaginatorModule,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class DashboardUsersComponent {
  private readonly selectAllCheckbox = viewChild<MatCheckbox>('selectAllCheckbox');

  private readonly dashboardService = inject(DashboardService);
  private readonly tokenService = inject(TokenService);
  private readonly snackBar = inject(MatSnackBar);
  private readonly dialog = inject(MatDialog);
  private readonly defaultAvatarPath = '/logo/logo.png';
  private readonly selectedUserIds = signal<Set<string>>(new Set<string>());
  private readonly rowActionInFlightIds = signal<Set<string>>(new Set<string>());
  protected readonly bulkActionInFlight = signal(false);

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
  protected readonly displayedColumns: ReadonlyArray<string> = [
    'select',
    'username',
    'email',
    'role',
    'status',
    'updatedAt',
    'actions',
  ];
  protected readonly pageSizeOptions: ReadonlyArray<number> = [10, 20, 50];

  protected readonly totalUsersCount = computed(() => this.usersResource.value()?.count ?? 0);

  protected readonly activeUsersCount = computed(() => this.users().filter((user) => user.status === 'active').length);

  protected readonly suspendedUsersCount = computed(() => this.users().filter((user) => user.status === 'suspended').length);

  protected readonly hasError = computed(() => Boolean(this.usersResource.error()));
  protected readonly currentActorRole = this.tokenService.role;
  protected readonly currentActorId = this.tokenService.userId;
  protected readonly selectedCount = computed(() => this.selectedUserIds().size);
  protected readonly anyRowActionInFlight = computed(() => this.rowActionInFlightIds().size > 0);
  protected readonly allOnPageSelected = computed(() => {
    const users = this.users();
    if (!users.length) return false;

    const selected = this.selectedUserIds();
    return users.every((user) => selected.has(user.id));
  });
  protected readonly someOnPageSelected = computed(() => {
    const users = this.users();
    if (!users.length) return false;

    const selected = this.selectedUserIds();
    const selectedOnPageCount = users.filter((user) => selected.has(user.id)).length;
    return selectedOnPageCount > 0 && selectedOnPageCount < users.length;
  });

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
    this.clearSelection();
  }

  protected onRoleFilterChange(event: MatSelectChange): void {
    this.roleFilter.set(event.value as 'all' | UserRole);
    this.currentPage.set(1);
    this.clearSelection();
  }

  protected onStatusFilterChange(event: MatSelectChange): void {
    this.statusFilter.set(event.value as 'all' | UserStatus);
    this.currentPage.set(1);
    this.clearSelection();
  }

  protected onSortChange(event: Sort): void {
    const direction = event.direction || 'desc';
    const active = (event.active || 'updatedAt') as 'username' | 'email' | 'role' | 'status' | 'createdAt' | 'updatedAt';

    this.sortBy.set(active);
    this.sortDirection.set(direction as 'asc' | 'desc');
    this.currentPage.set(1);
    this.clearSelection();
  }

  protected onPageChange(event: PageEvent): void {
    this.currentPage.set(event.pageIndex + 1);
    this.pageSize.set(event.pageSize);
    this.clearSelection();
  }

  protected onToggleAllRows(event: MatCheckboxChange): void {
    const users = this.users();
    const next = new Set(this.selectedUserIds());

    if (event.checked) {
      users.forEach((user) => {
        if (this.canSelectUser(user)) next.add(user.id);
      });
    } else {
      users.forEach((user) => next.delete(user.id));
    }

    this.selectedUserIds.set(next);
  }

  protected onToggleRowSelection(userId: string, checked: boolean): void {
    const next = new Set(this.selectedUserIds());
    if (checked) {
      next.add(userId);
    } else {
      next.delete(userId);
    }

    this.selectedUserIds.set(next);
  }

  protected isUserSelected(userId: string): boolean {
    return this.selectedUserIds().has(userId);
  }

  protected canSelectUser(user: { id: string; role: UserRole }): boolean {
    if (this.bulkActionInFlight()) return false;
    return this.canPerformRowAction(user);
  }

  protected isRowActionInFlight(userId: string): boolean {
    return this.rowActionInFlightIds().has(userId);
  }

  protected canPerformRowAction(user: { id: string; role: UserRole }): boolean {
    return this.canDeleteUser(user) || this.canManageStatus(user) || this.canManageRole(user);
  }

  protected canManageRole(user: { id: string; role: UserRole }): boolean {
    const actorRole = this.currentActorRole();
    if (actorRole !== 'admin' && actorRole !== 'super-admin') return false;
    if (this.currentActorId() === user.id) return false;
    if (actorRole === 'admin') return user.role !== 'super-admin';
    return true;
  }

  protected canManageStatus(user: { id: string; role: UserRole }): boolean {
    return this.canManageRole(user);
  }

  protected canDeleteUser(user: { id: string; role: UserRole }): boolean {
    const actorRole = this.currentActorRole();
    const actorId = this.currentActorId();

    if (!actorRole || !actorId || actorId === user.id) return false;

    switch (actorRole) {
      case 'super-admin':
        return true;
      case 'admin':
        return user.role !== 'super-admin';
      case 'moderator':
        return user.role === 'moderator' || user.role === 'user';
      default:
        return false;
    }
  }

  protected getAssignableRoles(user: { role: UserRole }): ReadonlyArray<UserRole> {
    const actorRole = this.currentActorRole();

    if (actorRole === 'super-admin') return ['super-admin', 'admin', 'moderator', 'user'];

    if (actorRole === 'admin') {
      if (user.role === 'super-admin') return [];
      return ['admin', 'moderator', 'user'];
    }

    return [];
  }

  protected async onChangeUserStatus(
    user: { id: string; username: string; role: UserRole; status: UserStatus },
    status: UserStatus,
  ): Promise<void> {
    if (!this.canManageStatus(user) || user.status === status || !this.beginRowAction(user.id)) {
      return;
    }

    try {
      await firstValueFrom(this.dashboardService.updateUserStatus(user.id, status));
      this.usersResource.reload();
      this.snackBar.open(`Η κατάσταση του χρήστη ${user.username} ενημερώθηκε.`, 'OK', { duration: 2400 });
    } catch {
      this.snackBar.open('Αποτυχία ενημέρωσης κατάστασης χρήστη.', 'OK', { duration: 3200 });
    } finally {
      this.endRowAction(user.id);
    }
  }

  protected async onChangeUserRole(user: { id: string; username: string; role: UserRole }, role: UserRole): Promise<void> {
    if (!this.canManageRole(user) || user.role === role || !this.beginRowAction(user.id)) return;

    try {
      await firstValueFrom(this.dashboardService.updateUserRole(user.id, role));
      this.usersResource.reload();
      this.snackBar.open(`Ο ρόλος του χρήστη ${user.username} ενημερώθηκε.`, 'OK', { duration: 2400 });
    } catch {
      this.snackBar.open('Αποτυχία ενημέρωσης ρόλου χρήστη.', 'OK', { duration: 3200 });
    } finally {
      this.endRowAction(user.id);
    }
  }

  protected async onDeleteUser(user: { id: string; username: string; role: UserRole }): Promise<void> {
    if (!this.canDeleteUser(user) || !this.beginRowAction(user.id)) return;

    try {
      const dialogRef = this.dialog.open(ConfirmDialogComponent, {
        width: '400px',
        disableClose: true,
        backdropClass: 'darker-backdrop',
        data: {
          title: 'Διαγραφή χρήστη',
          message: `Θέλετε σίγουρα να διαγράψετε τον χρήστη ${user.username}; Η ενέργεια δεν αναιρείται.`,
          confirmLabel: 'Διαγραφή',
          cancelLabel: 'Ακύρωση',
          isDangerous: true,
        },
      });

      const confirmed = await firstValueFrom(dialogRef.afterClosed());
      if (!confirmed) return;

      await firstValueFrom(this.dashboardService.deleteUser(user.id));
      this.clearSelection();
      this.usersResource.reload();
      this.snackBar.open(`Ο χρήστης ${user.username} διαγράφηκε.`, 'OK', { duration: 2600 });
    } catch {
      this.snackBar.open('Αποτυχία διαγραφής χρήστη.', 'OK', { duration: 3200 });
    } finally {
      this.endRowAction(user.id);
    }
  }

  protected async onBulkStatusChange(status: UserStatus): Promise<void> {
    if (this.bulkActionInFlight() || this.anyRowActionInFlight()) return;

    const selectedSet = this.selectedUserIds();
    if (selectedSet.size === 0) return;

    const targets = this.users()
      .filter((user) => selectedSet.has(user.id))
      .filter((user) => this.canManageStatus(user));

    if (!targets.length) {
      this.snackBar.open('Δεν υπάρχουν επιλέξιμοι χρήστες για αυτήν την ενέργεια.', 'OK', { duration: 3000 });
      return;
    }

    this.bulkActionInFlight.set(true);

    try {
      const actionLabel = status === 'active' ? 'ενεργοποίηση' : 'αναστολή';
      const dialogRef = this.dialog.open(ConfirmDialogComponent, {
        width: '420px',
        disableClose: true,
        backdropClass: 'darker-backdrop',
        data: {
          title: 'Μαζική ενημέρωση κατάστασης',
          message: `Θέλετε να εφαρμόσετε ${actionLabel} σε ${targets.length} χρήστες;`,
          confirmLabel: 'Επιβεβαίωση',
          cancelLabel: 'Ακύρωση',
        },
      });

      const confirmed = await firstValueFrom(dialogRef.afterClosed());
      if (!confirmed) return;

      const results = await Promise.allSettled(
        targets.map((user) => firstValueFrom(this.dashboardService.updateUserStatus(user.id, status))),
      );
      const successCount = results.filter((result) => result.status === 'fulfilled').length;
      const failCount = results.length - successCount;

      this.clearSelection();
      this.usersResource.reload();

      if (failCount === 0) {
        this.snackBar.open(`Η ενημέρωση κατάστασης εφαρμόστηκε σε ${successCount} χρήστες.`, 'OK', { duration: 3000 });
        return;
      }

      this.snackBar.open(`Ενημερώθηκαν ${successCount} χρήστες, απέτυχαν ${failCount}.`, 'OK', { duration: 3600 });
    } finally {
      this.bulkActionInFlight.set(false);
    }
  }

  private beginRowAction(userId: string): boolean {
    if (this.bulkActionInFlight()) return false;

    const current = this.rowActionInFlightIds();
    if (current.has(userId)) return false;

    const next = new Set(current);
    next.add(userId);
    this.rowActionInFlightIds.set(next);
    return true;
  }

  private endRowAction(userId: string): void {
    const current = this.rowActionInFlightIds();
    if (!current.has(userId)) return;

    const next = new Set(current);
    next.delete(userId);
    this.rowActionInFlightIds.set(next);
  }

  protected clearSelection(): void {
    if (this.selectedUserIds().size === 0) {
      this.resetHeaderSelectionCheckbox();
      return;
    }

    this.selectedUserIds.set(new Set<string>());
    this.resetHeaderSelectionCheckbox();
  }

  private resetHeaderSelectionCheckbox(): void {
    const selectAllCheckbox = this.selectAllCheckbox();
    if (!selectAllCheckbox) return;

    selectAllCheckbox.checked = false;
    selectAllCheckbox.indeterminate = false;
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
