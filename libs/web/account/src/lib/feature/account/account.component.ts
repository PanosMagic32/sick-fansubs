import { HttpErrorResponse } from '@angular/common/http';
import { ChangeDetectionStrategy, Component, OnInit, computed, effect, inject, signal } from '@angular/core';
import {
  type AbstractControl,
  FormBuilder,
  FormGroup,
  ReactiveFormsModule,
  type ValidationErrors,
  type ValidatorFn,
  Validators,
} from '@angular/forms';
import { Router } from '@angular/router';

import { MatCard, MatCardContent, MatCardHeader, MatCardTitle } from '@angular/material/card';
import { MatDivider } from '@angular/material/divider';
import { MatIcon } from '@angular/material/icon';
import { MatSnackBar } from '@angular/material/snack-bar';

import type { BlogPost } from '@shared/types';

import { StatusCardComponent, TokenService } from '@web/shared';

import { AccountFavoritesComponent } from '../../ui/favorites/account-favorites.component';
import { AccountProfileFormComponent } from '../../ui/profile-form/account-profile-form.component';
import { AccountProfileSummaryComponent } from '../../ui/profile-summary/account-profile-summary.component';
import { UserProfile, UserService, UpdateUserRequest } from '../../data-access/user.service';
import { AccountViewState } from '../../data-access/types';

@Component({
  selector: 'sf-account',
  templateUrl: './account.component.html',
  styleUrl: './account.component.scss',
  imports: [
    ReactiveFormsModule,
    MatCard,
    MatCardHeader,
    MatCardTitle,
    MatCardContent,
    MatIcon,
    MatDivider,
    StatusCardComponent,
    AccountFavoritesComponent,
    AccountProfileFormComponent,
    AccountProfileSummaryComponent,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class WebAccountComponent implements OnInit {
  private readonly tokenService = inject(TokenService);
  private readonly userService = inject(UserService);
  private readonly formBuilder = inject(FormBuilder);
  private readonly router = inject(Router);
  private readonly snackBar = inject(MatSnackBar);

  private readonly userId = signal<string>('');
  private readonly activeUserId = computed(() => (this.tokenService.isValidToken() ? this.userId() : ''));
  private readonly updateRequest = signal<UpdateUserRequest | null>(null);
  private readonly latestUpdatedProfile = signal<UserProfile | null>(null);
  private readonly latestFavoriteBlogPosts = signal<BlogPost[]>([]);
  private readonly favoriteRemovalRequest = signal<string | null>(null);
  private readonly avatarImageError = signal(false);
  private readonly showNewPassword = signal(false);
  private readonly showConfirmPassword = signal(false);

  readonly displayProfile = this.latestUpdatedProfile.asReadonly();
  readonly favoriteBlogPosts = this.latestFavoriteBlogPosts.asReadonly();
  readonly isNewPasswordVisible = this.showNewPassword.asReadonly();
  readonly isConfirmPasswordVisible = this.showConfirmPassword.asReadonly();

  protected readonly userProfile = this.userService.getUserProfile(this.activeUserId);
  protected readonly updateUserResource = this.userService.updateUserProfile(this.activeUserId, this.updateRequest);
  protected readonly favoriteBlogPostIdsResource = this.userService.getFavoriteBlogPostIds(this.activeUserId);
  protected readonly favoriteBlogPostsResource = this.userService.getFavoriteBlogPosts(this.activeUserId);
  protected readonly removeFavoriteResource = this.userService.removeFavoriteBlogPost(
    this.activeUserId,
    this.favoriteRemovalRequest,
  );

  protected profileForm!: FormGroup;

  readonly favoriteBlogPostIds = computed(() => this.displayProfile()?.favoriteBlogPostIds ?? []);

  readonly accountViewState = computed<AccountViewState>(() => {
    if (this.userProfile.isLoading()) return 'loading';
    if (this.userProfile.error()) return 'error';
    if (this.displayProfile()) return 'ready';
    return 'idle';
  });

  readonly safeAvatarUrl = computed(() => {
    const avatar = this.displayProfile()?.avatar;
    if (!avatar || this.avatarImageError()) return null;

    const parsed = this.parseUrl(avatar);
    if (!parsed || !this.isHttpProtocol(parsed.protocol)) return null;

    return parsed.href;
  });

  constructor() {
    effect(() => {
      if (!this.userProfile.hasValue()) return;

      const profile = this.userProfile.value();
      if (profile && this.profileForm) {
        this.latestUpdatedProfile.set(profile);
        this.profileForm.patchValue(profile);
        this.profileForm.markAsPristine();
        this.avatarImageError.set(false);
      }
    });

    effect(() => {
      if (!this.favoriteBlogPostIdsResource.hasValue()) return;

      const favoriteIds = this.favoriteBlogPostIdsResource.value()?.favoriteBlogPostIds;
      const profile = this.latestUpdatedProfile();

      if (!favoriteIds || !profile || this.haveSameIds(profile.favoriteBlogPostIds, favoriteIds)) {
        return;
      }

      this.latestUpdatedProfile.set({
        ...profile,
        favoriteBlogPostIds: [...favoriteIds],
      });
    });

    effect(() => {
      if (!this.favoriteBlogPostsResource.hasValue()) return;

      const favoritePosts = this.favoriteBlogPostsResource.value()?.posts;
      if (!favoritePosts) return;

      this.latestFavoriteBlogPosts.set([...favoritePosts]);
    });

    effect(() => {
      const profileError = this.userProfile.error();
      if (profileError instanceof HttpErrorResponse && profileError.status === 401) {
        this.tokenService.removeToken();
        this.router.navigate(['/auth/login'], { replaceUrl: true });
      }
    });

    effect(() => {
      const error = this.updateUserResource.error();
      if (error) {
        if (this.handleUnauthorized(error)) return;

        this.snackBar.open(this.getUpdateProfileErrorMessage(error), 'OK', { duration: 3500 });
        return;
      }

      if (!this.updateUserResource.hasValue()) return;

      const updatedProfile = this.updateUserResource.value();
      if (updatedProfile) {
        this.latestUpdatedProfile.set(updatedProfile);
        this.profileForm.patchValue(updatedProfile);
        this.snackBar.open('Το προφίλ ενημερώθηκε επιτυχώς', 'OK', { duration: 3000 });
        this.profileForm.markAsPristine();
        this.clearPasswordFields();
        this.updateRequest.set(null);
      }
    });

    effect(() => {
      const error = this.removeFavoriteResource.error();
      if (error) {
        if (this.handleUnauthorized(error)) return;

        this.snackBar.open(this.getRemoveFavoriteErrorMessage(error), 'OK', { duration: 3500 });
        this.favoriteRemovalRequest.set(null);
        return;
      }

      if (!this.removeFavoriteResource.hasValue()) return;

      const updatedFavorites = this.removeFavoriteResource.value()?.favoriteBlogPostIds;
      const profile = this.latestUpdatedProfile();
      if (!updatedFavorites || !profile) return;

      this.latestUpdatedProfile.set({
        ...profile,
        favoriteBlogPostIds: [...updatedFavorites],
      });
      this.latestFavoriteBlogPosts.set(
        this.latestFavoriteBlogPosts().filter((post) => post._id !== this.favoriteRemovalRequest()),
      );
      this.snackBar.open('Η ανάρτηση αφαιρέθηκε από τα αγαπημένα.', 'OK', { duration: 3000 });
      this.favoriteRemovalRequest.set(null);
    });
  }

  private parseUrl(url: string | undefined): URL | null {
    const value = url?.trim();
    if (!value) return null;

    try {
      return new URL(value);
    } catch {
      return null;
    }
  }

  private isHttpProtocol(protocol: string): boolean {
    return protocol === 'http:' || protocol === 'https:';
  }

  private isAllowedDownloadProtocol(protocol: string): boolean {
    return this.isHttpProtocol(protocol) || protocol === 'magnet:';
  }

  private handleUnauthorized(error: unknown): boolean {
    if (!(error instanceof HttpErrorResponse) || error.status !== 401) {
      return false;
    }

    this.tokenService.removeToken();
    this.router.navigate(['/auth/login'], { replaceUrl: true });
    return true;
  }

  private getUpdateProfileErrorMessage(error: unknown): string {
    if (!(error instanceof HttpErrorResponse)) {
      return 'Αποτυχία ενημέρωσης προφίλ. Δοκιμάστε ξανά.';
    }

    switch (error.status) {
      case 0:
        return 'Αδυναμία σύνδεσης με τον διακομιστή. Δοκιμάστε ξανά σε λίγο.';
      case 400:
        return 'Τα στοιχεία που δώσατε δεν είναι έγκυρα. Ελέγξτε τα πεδία και δοκιμάστε ξανά.';
      case 403:
        return 'Δεν έχετε δικαίωμα ενημέρωσης αυτού του προφίλ.';
      case 404:
        return 'Το προφίλ χρήστη δε βρέθηκε.';
      case 409:
        return 'Το email χρησιμοποιείται ήδη από άλλον λογαριασμό.';
      default:
        return 'Παρουσιάστηκε σφάλμα κατά την ενημέρωση. Δοκιμάστε ξανά.';
    }
  }

  private getRemoveFavoriteErrorMessage(error: unknown): string {
    if (!(error instanceof HttpErrorResponse)) {
      return 'Αποτυχία αφαίρεσης από τα αγαπημένα. Δοκιμάστε ξανά.';
    }

    switch (error.status) {
      case 0:
        return 'Αδυναμία σύνδεσης με τον διακομιστή. Δοκιμάστε ξανά σε λίγο.';
      case 400:
        return 'Μη έγκυρο αίτημα αφαίρεσης αγαπημένου.';
      case 403:
        return 'Δεν έχετε δικαίωμα αλλαγής των αγαπημένων αυτού του λογαριασμού.';
      case 404:
        return 'Η ανάρτηση ή ο λογαριασμός δε βρέθηκε.';
      default:
        return 'Παρουσιάστηκε σφάλμα κατά την αφαίρεση από τα αγαπημένα.';
    }
  }

  private haveSameIds(previousIds: string[], currentIds: string[]): boolean {
    if (previousIds.length !== currentIds.length) return false;
    return previousIds.every((id, index) => id === currentIds[index]);
  }

  ngOnInit() {
    this.initializeForm();
    this.loadUserProfile();
  }

  private loadUserProfile() {
    const userIdValue = this.tokenService.getUserId();

    if (!userIdValue || !this.tokenService.isValidToken()) {
      this.tokenService.removeToken();
      this.router.navigate(['/auth/login']);
      return;
    }

    this.userId.set(userIdValue);
  }

  private initializeForm() {
    this.profileForm = this.formBuilder.group(
      {
        username: [{ value: '', disabled: true }],
        email: ['', [Validators.required, Validators.email]],
        avatar: [''],
        password: ['', [this.optionalPasswordStrengthValidator()]],
        confirmPassword: [''],
      },
      { validators: [this.passwordsMatchValidator()] },
    );
  }

  private optionalPasswordStrengthValidator(): ValidatorFn {
    return (control: AbstractControl): ValidationErrors | null => {
      const value = String(control.value ?? '');

      if (!value) {
        return null;
      }

      const hasLower = /[a-z]/.test(value);
      const hasUpper = /[A-Z]/.test(value);
      const hasNumber = /\d/.test(value);
      const hasSymbol = /[^A-Za-z0-9]/.test(value);

      return {
        ...(value.length < 8 ? { minlength: true } : {}),
        ...(value.length > 128 ? { maxlength: true } : {}),
        ...(!hasLower ? { needsLowercase: true } : {}),
        ...(!hasUpper ? { needsUppercase: true } : {}),
        ...(!hasNumber ? { needsNumber: true } : {}),
        ...(!hasSymbol ? { needsSymbol: true } : {}),
      };
    };
  }

  private passwordsMatchValidator(): ValidatorFn {
    return (control: AbstractControl): ValidationErrors | null => {
      const password = control.get('password')?.value as string | undefined;
      const confirmPassword = control.get('confirmPassword')?.value as string | undefined;

      if (!password && !confirmPassword) {
        return null;
      }

      if (password && !confirmPassword) {
        return { confirmPasswordRequired: true };
      }

      if (!password && confirmPassword) {
        return { passwordRequired: true };
      }

      if (password !== confirmPassword) {
        return { passwordMismatch: true };
      }

      return null;
    };
  }

  private clearPasswordFields() {
    this.profileForm.patchValue({
      password: '',
      confirmPassword: '',
    });
    this.profileForm.updateValueAndValidity();
  }

  get passwordStrengthLabel(): string {
    const value = (this.profileForm.get('password')?.value as string | undefined) ?? '';

    if (!value) {
      return 'Συμπληρώστε νέο κωδικό μόνο αν θέλετε αλλαγή.';
    }

    if (value.length < 10) {
      return 'Ισχύς κωδικού: Αδύναμος';
    }

    const hasLower = /[a-z]/.test(value);
    const hasUpper = /[A-Z]/.test(value);
    const hasNumber = /\d/.test(value);
    const hasSymbol = /[^A-Za-z0-9]/.test(value);

    const score = [hasLower, hasUpper, hasNumber, hasSymbol].filter(Boolean).length;
    if (score >= 3) {
      return 'Ισχύς κωδικού: Ισχυρός';
    }

    return 'Ισχύς κωδικού: Μέτριος';
  }

  get passwordRequirementsHint(): string {
    return 'Ο νέος κωδικός πρέπει να έχει 8-128 χαρακτήρες και να περιέχει πεζό, κεφαλαίο, αριθμό και σύμβολο.';
  }

  get shouldShowPasswordMismatch(): boolean {
    const confirmControl = this.profileForm.get('confirmPassword');
    const hasMismatch = this.profileForm.hasError('passwordMismatch');
    return Boolean(hasMismatch && (confirmControl?.dirty || confirmControl?.touched));
  }

  toggleNewPasswordVisibility() {
    this.showNewPassword.update((value) => !value);
  }

  toggleConfirmPasswordVisibility() {
    this.showConfirmPassword.update((value) => !value);
  }

  onResetForm() {
    const profile = this.displayProfile();
    if (profile) {
      this.profileForm.patchValue(profile);
      this.clearPasswordFields();
      this.profileForm.markAsPristine();
    }
  }

  onSaveChanges() {
    const profile = this.displayProfile();
    if (this.profileForm.invalid || !profile) {
      if (this.profileForm.invalid) {
        this.profileForm.markAllAsTouched();
        this.snackBar.open('Ελέγξτε τα πεδία του κωδικού και δοκιμάστε ξανά.', 'OK', { duration: 3000 });
      }

      return;
    }

    const userIdValue = this.tokenService.getUserId();
    if (!userIdValue) {
      this.snackBar.open('Η συνεδρία έληξε. Συνδεθείτε ξανά.', 'OK', { duration: 3000 });
      return;
    }

    const { email, avatar, password } = this.profileForm.getRawValue();
    this.updateRequest.set({
      email,
      avatar,
      ...(password ? { password } : {}),
    });
  }

  get profileErrorMessage(): string {
    const error = this.userProfile.error();
    if (!error) return '';

    if (error instanceof HttpErrorResponse) {
      switch (error.status) {
        case 0:
          return 'Αδυναμία σύνδεσης με τον διακομιστή. Ελέγξτε τη σύνδεσή σας και δοκιμάστε ξανά.';
        case 401:
          return 'Η συνεδρία σας έχει λήξει. Αποσυνδεθείτε και συνδεθείτε ξανά.';
        case 403:
          return 'Δεν έχετε δικαίωμα πρόσβασης σε αυτό το προφίλ.';
        case 404:
          return 'Το προφίλ χρήστη δε βρέθηκε.';
        default:
          break;
      }
    }

    return 'Παρουσιάστηκε σφάλμα κατά τη φόρτωση του προφίλ. Δοκιμάστε ξανά.';
  }

  onRetryLoadProfile() {
    this.userProfile.reload();
    this.favoriteBlogPostIdsResource.reload();
    this.favoriteBlogPostsResource.reload();
  }

  onRetryLoadFavoriteIds() {
    this.favoriteBlogPostIdsResource.reload();
  }

  onRetryLoadFavoritePosts() {
    this.favoriteBlogPostsResource.reload();
  }

  onDownload(url: string | undefined) {
    const downloadURL = this.parseUrl(url);
    if (!downloadURL || !this.isAllowedDownloadProtocol(downloadURL.protocol)) {
      this.snackBar.open('Μη έγκυρος σύνδεσμος λήψης.', 'OK', { duration: 3000 });
      return;
    }

    const anchor = document.createElement('a');
    anchor.href = downloadURL.href;
    if (downloadURL.protocol !== 'magnet:') {
      anchor.download = downloadURL.pathname.split('/').pop() || '';
      anchor.target = '_blank';
      anchor.rel = 'noopener noreferrer';
    }
    anchor.click();
  }

  onAvatarImageError() {
    this.avatarImageError.set(true);
  }

  onRemoveFavorite(postId: string | undefined) {
    if (!postId || this.removeFavoriteResource.isLoading()) return;
    this.favoriteRemovalRequest.set(postId);
  }

  onLogout() {
    this.tokenService.removeToken();
    this.router.navigate(['/'], { replaceUrl: true });
  }
}
