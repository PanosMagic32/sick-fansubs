import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { RouterOutlet } from '@angular/router';

import { MatCard, MatCardContent, MatCardHeader, MatCardSubtitle, MatCardTitle } from '@angular/material/card';

import { TokenService } from '@web/shared';

@Component({
  selector: 'sf-dashboard-shell',
  templateUrl: './dashboard-shell.component.html',
  styleUrl: './dashboard-shell.component.scss',
  imports: [RouterOutlet, MatCard, MatCardHeader, MatCardTitle, MatCardSubtitle, MatCardContent],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class DashboardShellComponent {
  private readonly tokenService = inject(TokenService);

  protected readonly userRole = this.tokenService.role;
}
