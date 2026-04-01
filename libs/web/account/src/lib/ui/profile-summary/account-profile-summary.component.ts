import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import { MatIcon } from '@angular/material/icon';

import type { UserProfile } from '../../data-access/user.service';

@Component({
  selector: 'sf-account-profile-summary',
  templateUrl: './account-profile-summary.component.html',
  styleUrl: './account-profile-summary.component.scss',
  imports: [MatIcon],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AccountProfileSummaryComponent {
  readonly profile = input.required<UserProfile>();
  readonly safeAvatarUrl = input<string | null>(null);

  readonly avatarImageError = output<void>();

  onAvatarImageError(): void {
    this.avatarImageError.emit();
  }
}
