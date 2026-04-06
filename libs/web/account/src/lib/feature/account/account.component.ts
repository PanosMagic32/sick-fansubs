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
import { finalize, firstValueFrom } from 'rxjs';

import { MatCard, MatCardContent, MatCardHeader, MatCardTitle } from '@angular/material/card';
import { MatDialog } from '@angular/material/dialog';
import { MatDivider } from '@angular/material/divider';
import { MatIcon } from '@angular/material/icon';
import { MatSnackBar } from '@angular/material/snack-bar';
import { MatTabsModule } from '@angular/material/tabs';

import type { BlogPost, Project } from '@shared/types';

import { ConfirmDialogComponent, StatusCardComponent, TokenService } from '@web/shared';

import { AccountFavoritesComponent } from '../../ui/favorites/account-favorites.component';
import { AccountFavoriteProjectsComponent } from '../../ui/favorite-projects/account-favorite-projects.component';
import { AccountProfileFormComponent } from '../../ui/profile-form/account-profile-form.component';
import { AccountProfileSummaryComponent } from '../../ui/profile-summary/account-profile-summary.component';
import { UserService } from '../../data-access/user.service';
import {
  AccountViewState,
  FavoriteSortOrder,
  FavoritesPageChange,
  UpdateUserRequest,
  UserProfile,
} from '../../data-access/types';

const FAVORITES_DEFAULT_PAGE = 1;
const FAVORITES_DEFAULT_PAGE_SIZE = 12;
const FAVORITES_PAGE_SIZE_OPTIONS = [12, 24, 48];
const MAX_FILE_SIZE = 5 * 1024 * 1024; // 5 MB

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
    MatTabsModule,
    StatusCardComponent,
    AccountFavoritesComponent,
    AccountFavoriteProjectsComponent,
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
  private readonly dialog = inject(MatDialog);

  private readonly userId = signal<string>('');
  private readonly activeUserId = computed(() => (this.tokenService.isValidToken() ? this.userId() : ''));
  private readonly updateRequest = signal<UpdateUserRequest | null>(null);
  private readonly latestUpdatedProfile = signal<UserProfile | null>(null);
  private readonly latestFavoriteBlogPosts = signal<BlogPost[]>([]);
  private readonly latestFavoriteBlogPostsTotal = signal(0);
  private readonly latestFavoriteProjects = signal<Project[]>([]);
  private readonly latestFavoriteProjectsTotal = signal(0);
  private readonly favoriteRemovalRequest = signal<string | null>(null);
  private readonly favoriteProjectRemovalRequest = signal<string | null>(null);
  private readonly avatarImageError = signal(false);
  private readonly showNewPassword = signal(false);
  private readonly showConfirmPassword = signal(false);
  private readonly favoritePostsCurrentPage = signal(FAVORITES_DEFAULT_PAGE);
  private readonly favoritePostsPageSize = signal(FAVORITES_DEFAULT_PAGE_SIZE);
  private readonly favoriteProjectsCurrentPage = signal(FAVORITES_DEFAULT_PAGE);
  private readonly favoriteProjectsPageSize = signal(FAVORITES_DEFAULT_PAGE_SIZE);
  private readonly avatarUploadInProgress = signal(false);
  private readonly pendingUploadedAvatarUrl = signal<string | null>(null);
  private readonly favoriteSortOrder = signal<FavoriteSortOrder>('newest');
  private readonly favoriteProjectsSortOrder = signal<FavoriteSortOrder>('newest');
  private readonly deleteAccountRequest = signal(false); // Trigger signal for delete

  protected readonly displayProfile = this.latestUpdatedProfile.asReadonly();
  protected readonly favoriteBlogPosts = this.latestFavoriteBlogPosts.asReadonly();
  protected readonly favoriteBlogPostsTotal = this.latestFavoriteBlogPostsTotal.asReadonly();
  protected readonly favoriteProjects = this.latestFavoriteProjects.asReadonly();
  protected readonly favoriteProjectsTotal = this.latestFavoriteProjectsTotal.asReadonly();
  protected readonly favoritePostsPage = this.favoritePostsCurrentPage.asReadonly();
  protected readonly favoritePostsPageSizeValue = this.favoritePostsPageSize.asReadonly();
  protected readonly favoriteProjectsPage = this.favoriteProjectsCurrentPage.asReadonly();
  protected readonly favoriteProjectsPageSizeValue = this.favoriteProjectsPageSize.asReadonly();
  protected readonly favoritePostsPageSizeOptions = signal(FAVORITES_PAGE_SIZE_OPTIONS).asReadonly();
  protected readonly favoriteProjectsPageSizeOptions = signal(FAVORITES_PAGE_SIZE_OPTIONS).asReadonly();
  protected readonly isAvatarUploadInProgress = this.avatarUploadInProgress.asReadonly();
  protected readonly isNewPasswordVisible = this.showNewPassword.asReadonly();
  protected readonly isConfirmPasswordVisible = this.showConfirmPassword.asReadonly();
  protected readonly favoriteSortOrderValue = this.favoriteSortOrder.asReadonly();
  protected readonly favoriteProjectsSortOrderValue = this.favoriteProjectsSortOrder.asReadonly();

  protected readonly canAccessDashboard = computed(() => this.tokenService.canAccessDashboard());

  protected readonly userProfile = this.userService.getUserProfile(this.activeUserId);
  protected readonly updateUserResource = this.userService.updateUserProfile(this.activeUserId, this.updateRequest);
  protected readonly favoriteBlogPostIdsResource = this.userService.getFavoriteBlogPostIds(this.activeUserId);
  protected readonly favoriteProjectIdsResource = this.userService.getFavoriteProjectIds(this.activeUserId);

  protected readonly favoriteBlogPostsResource = this.userService.getFavoriteBlogPosts(
    this.activeUserId,
    this.favoritePostsPageSize,
    this.favoritePostsCurrentPage,
    this.favoriteSortOrder,
  );

  protected readonly favoriteProjectsResource = this.userService.getFavoriteProjects(
    this.activeUserId,
    this.favoriteProjectsPageSize,
    this.favoriteProjectsCurrentPage,
    this.favoriteProjectsSortOrder,
  );

  protected readonly removeFavoriteResource = this.userService.removeFavoriteBlogPost(
    this.activeUserId,
    this.favoriteRemovalRequest,
  );

  protected readonly removeFavoriteProjectResource = this.userService.removeFavoriteProject(
    this.activeUserId,
    this.favoriteProjectRemovalRequest,
  );

  protected readonly deleteAccountResource = this.userService.deleteAccount(
    computed(() => (this.deleteAccountRequest() ? this.activeUserId() : '')),
  );

  protected profileForm!: FormGroup;

  readonly favoriteBlogPostIds = computed(() => this.displayProfile()?.favoriteBlogPostIds ?? []);
  readonly favoriteProjectIds = computed(() => this.displayProfile()?.favoriteProjectIds ?? []);

  readonly accountViewState = computed<AccountViewState>(() => {
    if (this.userProfile.isLoading()) return 'loading';
    if (this.userProfile.error()) return 'error';
    if (this.displayProfile()) return 'ready';
    return 'idle';
  });

  readonly safeAvatarUrl = computed(() => {
    const avatarFromForm = String(this.profileForm?.get('avatar')?.value ?? '').trim();
    const avatar = avatarFromForm || this.pendingUploadedAvatarUrl() || this.displayProfile()?.avatar;
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
      if (!this.favoriteProjectIdsResource.hasValue()) return;

      const favoriteIds = this.favoriteProjectIdsResource.value()?.favoriteProjectIds;
      const profile = this.latestUpdatedProfile();

      if (!favoriteIds || !profile || this.haveSameIds(profile.favoriteProjectIds, favoriteIds)) {
        return;
      }

      this.latestUpdatedProfile.set({
        ...profile,
        favoriteProjectIds: [...favoriteIds],
      });
    });

    effect(() => {
      if (!this.favoriteBlogPostsResource.hasValue()) return;

      const favoritePosts = this.favoriteBlogPostsResource.value()?.posts;
      if (!favoritePosts) return;

      this.latestFavoriteBlogPosts.set([...favoritePosts]);

      const count = this.favoriteBlogPostsResource.value()?.count;
      this.latestFavoriteBlogPostsTotal.set(count ?? 0);
    });

    effect(() => {
      if (!this.favoriteProjectsResource.hasValue()) return;

      const favoriteProjects = this.favoriteProjectsResource.value()?.projects;
      if (!favoriteProjects) return;

      this.latestFavoriteProjects.set([...favoriteProjects]);

      const count = this.favoriteProjectsResource.value()?.count;
      this.latestFavoriteProjectsTotal.set(count ?? 0);
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
        this.pendingUploadedAvatarUrl.set(null);
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

      const totalFavorites = updatedFavorites.length;
      const maxPage = Math.max(1, Math.ceil(totalFavorites / this.favoritePostsPageSize()));
      if (this.favoritePostsCurrentPage() > maxPage) {
        this.favoritePostsCurrentPage.set(maxPage);
      } else {
        this.favoriteBlogPostsResource.reload();
      }

      this.snackBar.open('Η ανάρτηση αφαιρέθηκε από τα αγαπημένα.', 'OK', { duration: 3000 });
      this.favoriteRemovalRequest.set(null);
    });

    effect(() => {
      const error = this.removeFavoriteProjectResource.error();
      if (error) {
        if (this.handleUnauthorized(error)) return;

        this.snackBar.open(this.getRemoveFavoriteProjectErrorMessage(error), 'OK', { duration: 3500 });
        this.favoriteProjectRemovalRequest.set(null);
        return;
      }

      if (!this.removeFavoriteProjectResource.hasValue()) return;

      const updatedFavorites = this.removeFavoriteProjectResource.value()?.favoriteProjectIds;
      const profile = this.latestUpdatedProfile();
      if (!updatedFavorites || !profile) return;

      this.latestUpdatedProfile.set({
        ...profile,
        favoriteProjectIds: [...updatedFavorites],
      });

      const totalFavorites = updatedFavorites.length;
      const maxPage = Math.max(1, Math.ceil(totalFavorites / this.favoriteProjectsPageSize()));
      if (this.favoriteProjectsCurrentPage() > maxPage) {
        this.favoriteProjectsCurrentPage.set(maxPage);
      } else {
        this.favoriteProjectsResource.reload();
      }

      this.snackBar.open('Το project αφαιρέθηκε από τα αγαπημένα.', 'OK', { duration: 3000 });
      this.favoriteProjectRemovalRequest.set(null);
    });

    effect(() => {
      const error = this.deleteAccountResource.error();
      if (error) {
        if (this.handleUnauthorized(error)) {
          this.deleteAccountRequest.set(false);
          return;
        }

        this.snackBar.open(this.getDeleteAccountErrorMessage(error), 'OK', { duration: 3500 });
        this.deleteAccountRequest.set(false);
        return;
      }

      if (!this.deleteAccountResource.hasValue()) return;

      // Account deleted successfully
      this.snackBar.open('Ο λογαριασμός διαγράφηκε επιτυχώς.', 'OK', { duration: 2000 });
      this.deleteAccountRequest.set(false);

      // Force logout and redirect to home
      this.tokenService.logout().subscribe(() => {
        this.router.navigate(['/'], { replaceUrl: true });
      });
    });
  }

  private parseUrl(url: string | undefined): URL | null {
    const value = url?.trim();
    if (!value) return null;

    try {
      return new URL(value, window.location.origin);
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

  private getRemoveFavoriteProjectErrorMessage(error: unknown): string {
    if (!(error instanceof HttpErrorResponse)) {
      return 'Αποτυχία αφαίρεσης project από τα αγαπημένα. Δοκιμάστε ξανά.';
    }

    switch (error.status) {
      case 0:
        return 'Αδυναμία σύνδεσης με τον διακομιστή. Δοκιμάστε ξανά σε λίγο.';
      case 400:
        return 'Μη έγκυρο αίτημα αφαίρεσης αγαπημένου project.';
      case 403:
        return 'Δεν έχετε δικαίωμα αλλαγής των αγαπημένων projects αυτού του λογαριασμού.';
      case 404:
        return 'Το project ή ο λογαριασμός δε βρέθηκε.';
      default:
        return 'Παρουσιάστηκε σφάλμα κατά την αφαίρεση project από τα αγαπημένα.';
    }
  }

  private getAvatarUploadErrorMessage(error: unknown): string {
    if (!(error instanceof HttpErrorResponse)) {
      return 'Αποτυχία ανεβάσματος εικόνας. Δοκιμάστε ξανά.';
    }

    switch (error.status) {
      case 0:
        return 'Αδυναμία σύνδεσης με τον διακομιστή. Δοκιμάστε ξανά σε λίγο.';
      case 400:
        return 'Το αρχείο δεν είναι έγκυρο. Επιλέξτε εικόνα έως 5MB (jpg, png, gif, webp).';
      case 401:
        return 'Η συνεδρία σας έληξε. Συνδεθείτε ξανά.';
      default:
        return 'Παρουσιάστηκε σφάλμα κατά το ανέβασμα εικόνας.';
    }
  }

  private getDeleteAccountErrorMessage(error: unknown): string {
    if (!(error instanceof HttpErrorResponse)) {
      return 'Αποτυχία διαγραφής λογαριασμού. Δοκιμάστε ξανά.';
    }

    switch (error.status) {
      case 0:
        return 'Αδυναμία σύνδεσης με τον διακομιστή. Δοκιμάστε ξανά σε λίγο.';
      case 400:
        return 'Μη έγκυρο αίτημα διαγραφής.';
      case 403:
        return 'Δεν έχετε δικαίωμα διαγραφής αυτού του λογαριασμού.';
      case 404:
        return 'Ο λογαριασμός δε βρέθηκε.';
      default:
        return 'Παρουσιάστηκε σφάλμα κατά τη διαγραφή. Δοκιμάστε ξανά.';
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
      this.pendingUploadedAvatarUrl.set(null);
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
    const manualAvatarUrl = String(avatar ?? '').trim();
    const uploadedAvatarUrl = this.pendingUploadedAvatarUrl();

    if (manualAvatarUrl && uploadedAvatarUrl) {
      this.snackBar.open('Το URL εικόνας θα αντικαταστήσει την ανεβασμένη εικόνα.', 'OK', { duration: 3200 });
    }

    const nextAvatar = manualAvatarUrl || uploadedAvatarUrl || undefined;
    this.updateRequest.set({
      email,
      ...(nextAvatar ? { avatar: nextAvatar } : {}),
      ...(password ? { password } : {}),
    });
  }

  onAvatarFileSelected(file: File) {
    if (file.size > MAX_FILE_SIZE) {
      this.snackBar.open('Το αρχείο δεν μπορεί να υπερβαίνει τα 5 MB.', 'OK', { duration: 3000 });
      return;
    }

    this.avatarUploadInProgress.set(true);

    this.userService
      .uploadAvatarImage(file)
      .pipe(finalize(() => this.avatarUploadInProgress.set(false)))
      .subscribe({
        next: (response) => {
          this.pendingUploadedAvatarUrl.set(response.url);
          // Keep URL input free for manual override, while preview uses uploaded URL.
          this.profileForm.patchValue({ avatar: '' });
          this.profileForm.markAsDirty();
          this.avatarImageError.set(false);
          this.snackBar.open('Η εικόνα ανέβηκε. Πατήστε αποθήκευση για να ενημερωθεί το προφίλ.', 'OK', { duration: 3000 });
        },
        error: (error: unknown) => {
          if (this.handleUnauthorized(error)) return;
          this.snackBar.open(this.getAvatarUploadErrorMessage(error), 'OK', { duration: 3500 });
        },
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
    this.favoriteProjectIdsResource.reload();
    this.favoriteProjectsResource.reload();
  }

  onRetryLoadFavoriteIds() {
    this.favoriteBlogPostIdsResource.reload();
  }

  onRetryLoadFavoritePosts() {
    this.favoriteBlogPostsResource.reload();
  }

  onRetryLoadFavoriteProjectIds() {
    this.favoriteProjectIdsResource.reload();
  }

  onRetryLoadFavoriteProjects() {
    this.favoriteProjectsResource.reload();
  }

  onFavoritePostsPageChange(pageChange: FavoritesPageChange) {
    this.favoritePostsCurrentPage.set(pageChange.page);

    if (this.favoritePostsPageSize() !== pageChange.pageSize) {
      this.favoritePostsPageSize.set(pageChange.pageSize);
    }
  }

  onFavoriteProjectsPageChange(pageChange: FavoritesPageChange) {
    this.favoriteProjectsCurrentPage.set(pageChange.page);

    if (this.favoriteProjectsPageSize() !== pageChange.pageSize) {
      this.favoriteProjectsPageSize.set(pageChange.pageSize);
    }
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

  onRemoveFavoriteProject(projectId: string | undefined) {
    if (!projectId || this.removeFavoriteProjectResource.isLoading()) return;
    this.favoriteProjectRemovalRequest.set(projectId);
  }

  onLogout() {
    this.tokenService.logout().subscribe(() => {
      this.router.navigate(['/'], { replaceUrl: true });
    });
  }

  onOpenDashboard() {
    this.router.navigate(['/dashboard']);
  }

  onOpenFavoriteProject(projectId: string | undefined) {
    if (!projectId) return;
    this.router.navigate(['/projects', projectId]);
  }

  isFormDirty(): boolean {
    return this.profileForm.dirty;
  }

  onDeleteAccount() {
    const dialogRef = this.dialog.open(ConfirmDialogComponent, {
      width: '400px',
      disableClose: true,
      backdropClass: 'darker-backdrop',
      data: {
        title: 'Διαγραφή λογαριασμού',
        message: 'Αυτή η ενέργεια δεν μπορεί να αναιρεθεί. Όλα τα δεδομένα σας θα διαγραφούν. Είστε σίγουροι;',
        confirmLabel: 'Διαγραφή',
        cancelLabel: 'Ακύρωση',
        isDangerous: true,
      },
    });

    firstValueFrom(dialogRef.afterClosed()).then((result) => {
      if (!result) return;

      const userIdValue = this.tokenService.getUserId();
      if (!userIdValue) {
        this.snackBar.open('Η συνεδρία έληξε. Συνδεθείτε ξανά.', 'OK', { duration: 3000 });
        return;
      }

      // Set the signal to trigger the deleteAccountResource
      this.deleteAccountRequest.set(true);
    });
  }

  toggleFavoriteSortOrder() {
    this.favoritePostsCurrentPage.set(FAVORITES_DEFAULT_PAGE);
    this.favoriteSortOrder.update((order) => (order === 'newest' ? 'oldest' : 'newest'));
  }

  toggleFavoriteProjectsSortOrder() {
    this.favoriteProjectsCurrentPage.set(FAVORITES_DEFAULT_PAGE);
    this.favoriteProjectsSortOrder.update((order) => (order === 'newest' ? 'oldest' : 'newest'));
  }
}
