import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';
import { FormGroup, ReactiveFormsModule } from '@angular/forms';

import { MatButton, MatIconButton } from '@angular/material/button';
import { MatError, MatFormField, MatLabel } from '@angular/material/form-field';
import { MatIcon } from '@angular/material/icon';
import { MatInput } from '@angular/material/input';

@Component({
  selector: 'sf-account-profile-form',
  templateUrl: './account-profile-form.component.html',
  styleUrl: './account-profile-form.component.scss',
  imports: [ReactiveFormsModule, MatFormField, MatLabel, MatInput, MatError, MatButton, MatIconButton, MatIcon],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AccountProfileFormComponent {
  readonly profileForm = input.required<FormGroup>();
  readonly isNewPasswordVisible = input.required<boolean>();
  readonly isConfirmPasswordVisible = input.required<boolean>();
  readonly passwordRequirementsHint = input.required<string>();
  readonly passwordStrengthLabel = input.required<string>();
  readonly shouldShowPasswordMismatch = input.required<boolean>();
  readonly isSaving = input.required<boolean>();

  readonly toggleNewPasswordVisibility = output<void>();
  readonly toggleConfirmPasswordVisibility = output<void>();
  readonly saveChanges = output<void>();
  readonly resetForm = output<void>();
  readonly logout = output<void>();

  onToggleNewPasswordVisibility(): void {
    this.toggleNewPasswordVisibility.emit();
  }

  onToggleConfirmPasswordVisibility(): void {
    this.toggleConfirmPasswordVisibility.emit();
  }

  onSaveChanges(): void {
    this.saveChanges.emit();
  }

  onResetForm(): void {
    this.resetForm.emit();
  }

  onLogout(): void {
    this.logout.emit();
  }
}
