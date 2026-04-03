import { ChangeDetectionStrategy, Component } from '@angular/core';

import { MatCard, MatCardContent, MatCardHeader, MatCardTitle } from '@angular/material/card';

@Component({
  selector: 'sf-dashboard-users',
  templateUrl: './dashboard-users.component.html',
  styleUrl: './dashboard-users.component.scss',
  imports: [MatCard, MatCardHeader, MatCardTitle, MatCardContent],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class DashboardUsersComponent {}
