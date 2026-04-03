import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { Breakpoints, BreakpointObserver } from '@angular/cdk/layout';
import { map } from 'rxjs';

import { MatCard, MatCardContent, MatCardHeader, MatCardSubtitle, MatCardTitle } from '@angular/material/card';
import { MatGridList, MatGridTile } from '@angular/material/grid-list';

@Component({
  selector: 'sf-dashboard-home',
  templateUrl: './dashboard-home.component.html',
  styleUrl: './dashboard-home.component.scss',
  imports: [MatGridList, MatGridTile, MatCard, MatCardHeader, MatCardTitle, MatCardSubtitle, MatCardContent],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class DashboardHomeComponent {
  private readonly breakpointObserver = inject(BreakpointObserver);

  protected readonly cards = toSignal(
    this.breakpointObserver.observe(Breakpoints.Handset).pipe(
      map(({ matches }) => {
        if (matches) {
          return [
            { title: 'Συνολικοί Χρήστες', subtitle: '0', cols: 1, rows: 1 },
            { title: 'Ενεργοί Χρήστες', subtitle: '0', cols: 1, rows: 1 },
            { title: 'Χρήστες Σε Αναστολή', subtitle: '0', cols: 1, rows: 1 },
            { title: 'Μέλη Sick HQ', subtitle: '0', cols: 1, rows: 1 },
          ];
        }

        return [
          { title: 'Συνολικοί Χρήστες', subtitle: '0', cols: 2, rows: 1 },
          { title: 'Ενεργοί Χρήστες', subtitle: '0', cols: 1, rows: 1 },
          { title: 'Χρήστες Σε Αναστολή', subtitle: '0', cols: 1, rows: 1 },
          { title: 'Μέλη Sick HQ', subtitle: '0', cols: 2, rows: 1 },
        ];
      }),
    ),
    { initialValue: [] },
  );
}
