import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import { MatButton } from '@angular/material/button';
import { MatIcon } from '@angular/material/icon';

import { UserProfile } from '../../data-access/types';

@Component({
  selector: 'sf-account-profile-summary',
  templateUrl: './account-profile-summary.component.html',
  styleUrl: './account-profile-summary.component.scss',
  imports: [MatButton, MatIcon],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AccountProfileSummaryComponent {
  readonly profile = input.required<UserProfile>();
  readonly safeAvatarUrl = input<string | null>(null);
  readonly canAccessDashboard = input(false);

  readonly avatarImageError = output<void>();
  readonly openDashboard = output<void>();

  readonly roleLabelMap: Record<string, string> = {
    'super-admin': 'Υπερδιαχειριστής',
    admin: 'Διαχειριστής',
    moderator: 'Συντονιστής',
    user: 'Χρήστης',
  };

  get roleLabel(): string {
    const role = this.profile().role;
    if (role) return this.roleLabelMap[role] ?? 'Χρήστης';
    return 'Χρήστης';
  }

  onAvatarImageError(): void {
    this.avatarImageError.emit();
  }

  onOpenDashboard(): void {
    this.openDashboard.emit();
  }
}
