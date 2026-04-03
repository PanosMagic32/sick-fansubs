import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { Breakpoints, BreakpointObserver } from '@angular/cdk/layout';
import { map } from 'rxjs';

import { MatCard, MatCardContent, MatCardHeader, MatCardSubtitle, MatCardTitle } from '@angular/material/card';
import { MatGridList, MatGridTile } from '@angular/material/grid-list';

import { DashboardService } from '../../data-access/dashboard.service';

interface HomeMetricCard {
  title: string;
  subtitle: string;
  value: string;
  cols: number;
  rows: number;
}

@Component({
  selector: 'sf-dashboard-home',
  templateUrl: './dashboard-home.component.html',
  styleUrl: './dashboard-home.component.scss',
  imports: [MatGridList, MatGridTile, MatCard, MatCardHeader, MatCardTitle, MatCardSubtitle, MatCardContent],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class DashboardHomeComponent {
  private readonly breakpointObserver = inject(BreakpointObserver);
  private readonly dashboardService = inject(DashboardService);

  protected readonly usersResource = this.dashboardService.getUsers();

  protected readonly metrics = computed(() => {
    const users = this.usersResource.value() ?? [];
    const now = Date.now();
    const thirtyDaysMs = 30 * 24 * 60 * 60 * 1000;

    const activeUsers = users.filter((user) => user.status === 'active').length;
    const suspendedUsers = users.filter((user) => user.status === 'suspended').length;
    const staffMembers = users.filter((user) => user.role !== 'user').length;
    const superAdmins = users.filter((user) => user.role === 'super-admin').length;
    const newLast30Days = users.filter((user) => {
      if (!user.createdAt) return false;
      const createdAtMs = new Date(user.createdAt).getTime();
      if (Number.isNaN(createdAtMs)) return false;
      return now - createdAtMs <= thirtyDaysMs;
    }).length;

    return {
      totalUsers: users.length,
      activeUsers,
      suspendedUsers,
      staffMembers,
      superAdmins,
      newLast30Days,
    };
  });

  protected readonly cards = computed<ReadonlyArray<HomeMetricCard>>(() => {
    const metric = this.metrics();
    const isHandset = this.isHandset();

    if (isHandset) {
      return [
        {
          title: 'Συνολικοί Χρήστες',
          subtitle: 'Συνολικό μέγεθος κοινότητας',
          value: String(metric.totalUsers),
          cols: 1,
          rows: 1,
        },
        {
          title: 'Ενεργοί Χρήστες',
          subtitle: 'Λογαριασμοί σε ενεργή κατάσταση',
          value: String(metric.activeUsers),
          cols: 1,
          rows: 1,
        },
        {
          title: 'Χρήστες Σε Αναστολή',
          subtitle: 'Λογαριασμοί με περιορισμένη πρόσβαση',
          value: String(metric.suspendedUsers),
          cols: 1,
          rows: 1,
        },
        {
          title: 'Μέλη Staff',
          subtitle: 'super-admin, admin, moderator',
          value: String(metric.staffMembers),
          cols: 1,
          rows: 1,
        },
        {
          title: 'Νέα Μέλη (30 ημέρες)',
          subtitle: 'Πρόσφατες εγγραφές',
          value: String(metric.newLast30Days),
          cols: 1,
          rows: 1,
        },
        {
          title: 'Super Admins',
          subtitle: 'Κρίσιμοι λογαριασμοί διαχείρισης',
          value: String(metric.superAdmins),
          cols: 1,
          rows: 1,
        },
      ];
    }

    return [
      {
        title: 'Συνολικοί Χρήστες',
        subtitle: 'Συνολικό μέγεθος κοινότητας',
        value: String(metric.totalUsers),
        cols: 2,
        rows: 1,
      },
      {
        title: 'Ενεργοί Χρήστες',
        subtitle: 'Λογαριασμοί σε ενεργή κατάσταση',
        value: String(metric.activeUsers),
        cols: 1,
        rows: 1,
      },
      {
        title: 'Χρήστες Σε Αναστολή',
        subtitle: 'Λογαριασμοί με περιορισμένη πρόσβαση',
        value: String(metric.suspendedUsers),
        cols: 1,
        rows: 1,
      },
      {
        title: 'Μέλη Staff',
        subtitle: 'super-admin, admin, moderator',
        value: String(metric.staffMembers),
        cols: 1,
        rows: 1,
      },
      {
        title: 'Νέα Μέλη (30 ημέρες)',
        subtitle: 'Πρόσφατες εγγραφές',
        value: String(metric.newLast30Days),
        cols: 1,
        rows: 1,
      },
      {
        title: 'Super Admins',
        subtitle: 'Κρίσιμοι λογαριασμοί διαχείρισης',
        value: String(metric.superAdmins),
        cols: 2,
        rows: 1,
      },
    ];
  });

  protected readonly isHandset = toSignal(
    this.breakpointObserver.observe(Breakpoints.Handset).pipe(map(({ matches }) => matches)),
    { initialValue: false },
  );
}
