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
  readonly isUploadingAvatar = input<boolean>(false);
  readonly isNewPasswordVisible = input.required<boolean>();
  readonly isConfirmPasswordVisible = input.required<boolean>();
  readonly passwordRequirementsHint = input.required<string>();
  readonly passwordStrengthLabel = input.required<string>();
  readonly shouldShowPasswordMismatch = input.required<boolean>();
  readonly isSaving = input.required<boolean>();

  readonly toggleNewPasswordVisibility = output<void>();
  readonly toggleConfirmPasswordVisibility = output<void>();
  readonly avatarFileSelected = output<File>();
  readonly saveChanges = output<void>();
  readonly resetForm = output<void>();
  readonly logout = output<void>();
  readonly deleteAccount = output<void>();

  selectedAvatarFileName: string | null = null;

  onToggleNewPasswordVisibility(): void {
    this.toggleNewPasswordVisibility.emit();
  }

  onToggleConfirmPasswordVisibility(): void {
    this.toggleConfirmPasswordVisibility.emit();
  }

  onAvatarFileSelected(event: Event): void {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];

    if (file) {
      this.selectedAvatarFileName = file.name;
      this.avatarFileSelected.emit(file);
    } else {
      this.selectedAvatarFileName = null;
    }

    input.value = '';
  }

  onSaveChanges(): void {
    this.saveChanges.emit();
  }

  onResetForm(): void {
    this.selectedAvatarFileName = null;
    this.resetForm.emit();
  }

  onLogout(): void {
    this.logout.emit();
  }

  onDeleteAccount(): void {
    this.deleteAccount.emit();
  }
}
