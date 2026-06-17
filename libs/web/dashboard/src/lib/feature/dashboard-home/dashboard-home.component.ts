import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { Breakpoints, BreakpointObserver } from '@angular/cdk/layout';
import { map } from 'rxjs';

import { MatCard, MatCardContent, MatCardHeader, MatCardSubtitle, MatCardTitle } from '@angular/material/card';
import { MatGridList, MatGridTile } from '@angular/material/grid-list';

import { DashboardMetrics, DashboardService } from '../../data-access/dashboard.service';

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

  protected readonly statsResource = this.dashboardService.getStats();

  protected readonly metrics = computed<DashboardMetrics>(
    () =>
      this.statsResource.value() ?? {
        totalUsers: 0,
        activeUsers: 0,
        suspendedUsers: 0,
        staffMembers: 0,
        superAdmins: 0,
        adminCount: 0,
        moderatorCount: 0,
        newLast30Days: 0,
      },
  );

  protected readonly cards = computed<HomeMetricCard[]>(() => {
    const metric = this.metrics();
    const isHandset = this.isHandset();

    return [
      {
        title: 'Συνολικοί Χρήστες',
        subtitle: 'Συνολικό μέγεθος κοινότητας',
        value: String(metric.totalUsers),
        cols: isHandset ? 1 : 2,
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
        subtitle: 'Συνολικός αριθμός προσωπικού',
        value: String(metric.staffMembers),
        cols: 1,
        rows: 1,
      },
      {
        title: 'Super Admins',
        subtitle: 'Κρίσιμοι λογαριασμοί διαχείρισης',
        value: String(metric.superAdmins),
        cols: isHandset ? 1 : 2,
        rows: 1,
      },
      {
        title: 'Admins',
        subtitle: 'Διαχειριστές περιεχομένου',
        value: String(metric.adminCount),
        cols: 1,
        rows: 1,
      },
      {
        title: 'Moderators',
        subtitle: 'Συντονιστές κοινότητας',
        value: String(metric.moderatorCount),
        cols: 1,
        rows: 1,
      },
      {
        title: 'Νέα Μέλη (30 ημέρες)',
        subtitle: 'Πρόσφατες εγγραφές',
        value: String(metric.newLast30Days),
        cols: isHandset ? 1 : 2,
        rows: 1,
      },
    ];
  });

  protected readonly isHandset = toSignal(
    this.breakpointObserver.observe(Breakpoints.Handset).pipe(map(({ matches }) => matches)),
    { initialValue: false },
  );
}
