import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import { MatButton } from '@angular/material/button';
import { MatCard, MatCardContent, MatCardHeader, MatCardTitle } from '@angular/material/card';
import { MatIcon } from '@angular/material/icon';
import { MatProgressSpinner } from '@angular/material/progress-spinner';

type StatusCardState = 'loading' | 'error';
type StatusCardActionColor = 'primary' | 'accent' | 'warn';

@Component({
  selector: 'sf-status-card',
  templateUrl: './status-card.component.html',
  styleUrl: './status-card.component.scss',
  imports: [MatCard, MatCardHeader, MatCardTitle, MatCardContent, MatIcon, MatProgressSpinner, MatButton],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class StatusCardComponent {
  readonly state = input.required<StatusCardState>();
  readonly message = input.required<string>();
  readonly title = input<string>('');

  readonly showRetry = input<boolean>(false);
  readonly retryLabel = input<string>('Προσπαθήστε ξανά');
  readonly retryIcon = input<string>('refresh');

  readonly showSecondaryAction = input<boolean>(false);
  readonly secondaryActionLabel = input<string>('');
  readonly secondaryActionIcon = input<string>('logout');
  readonly secondaryActionColor = input<StatusCardActionColor>('warn');

  readonly retry = output<void>();
  readonly secondaryAction = output<void>();

  onRetry(): void {
    this.retry.emit();
  }

  onSecondaryAction(): void {
    this.secondaryAction.emit();
  }
}
