import { ChangeDetectionStrategy, Component } from '@angular/core';

import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';

import { MatIcon } from '@angular/material/icon';
import { MatTabLink, MatTabNav, MatTabNavPanel } from '@angular/material/tabs';

interface DashboardTab {
  readonly label: string;
  readonly icon: string;
  readonly path: string;
  readonly exact?: boolean;
}

@Component({
  selector: 'sf-dashboard-navigation',
  templateUrl: './dashboard-navigation.component.html',
  styleUrl: './dashboard-navigation.component.scss',
  imports: [RouterOutlet, RouterLink, RouterLinkActive, MatIcon, MatTabNav, MatTabLink, MatTabNavPanel],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class DashboardNavigationComponent {
  protected readonly tabs: readonly DashboardTab[] = [
    { label: 'Επισκόπηση', icon: 'dashboard', path: '/dashboard', exact: true },
    { label: 'Χρήστες', icon: 'groups', path: '/dashboard/users' },
  ];
}
