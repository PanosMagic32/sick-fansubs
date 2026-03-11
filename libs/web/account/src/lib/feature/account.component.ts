import { CommonModule } from '@angular/common';
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

import { MatButton, MatIconButton } from '@angular/material/button';
import { MatCard, MatCardContent, MatCardHeader, MatCardTitle } from '@angular/material/card';
import { MatDivider } from '@angular/material/divider';
import { MatError, MatFormField, MatLabel } from '@angular/material/form-field';
import { MatIcon } from '@angular/material/icon';
import { MatInput } from '@angular/material/input';
import { MatMenuModule } from '@angular/material/menu';
import { MatProgressSpinner } from '@angular/material/progress-spinner';
import { MatSnackBar } from '@angular/material/snack-bar';

import type { BlogPost } from '@shared/types';

import { TokenService } from '@web/shared';

import { UserProfile, UserService, UpdateUserRequest } from '../data-access/user.service';

@Component({
  selector: 'sf-account',
  templateUrl: './account.component.html',
  styleUrl: './account.component.scss',
  imports: [
    CommonModule,
    ReactiveFormsModule,
    MatCard,
    MatCardHeader,
    MatCardTitle,
    MatCardContent,
    MatFormField,
    MatLabel,
    MatInput,
    MatError,
    MatButton,
    MatIconButton,
    MatIcon,
    MatDivider,
    MatMenuModule,
    MatProgressSpinner,
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

  readonly displayProfile = this.latestUpdatedProfile.asReadonly();
  readonly favoriteBlogPosts = this.latestFavoriteBlogPosts.asReadonly();

  userProfile = this.userService.getUserProfile(this.activeUserId);
  updateUserResource = this.userService.updateUserProfile(this.activeUserId, this.updateRequest);
  favoriteBlogPostIdsResource = this.userService.getFavoriteBlogPostIds(this.activeUserId);
  favoriteBlogPostsResource = this.userService.getFavoriteBlogPosts(this.activeUserId);
  removeFavoriteResource = this.userService.removeFavoriteBlogPost(this.activeUserId, this.favoriteRemovalRequest);

  readonly favoriteBlogPostIds = computed(() => this.displayProfile()?.favoriteBlogPostIds ?? []);

  profileForm!: FormGroup;

  constructor() {
    effect(() => {
      if (!this.userProfile.hasValue()) {
        return;
      }

      const profile = this.userProfile.value();
      if (profile && this.profileForm) {
        this.latestUpdatedProfile.set(profile);
        this.profileForm.patchValue(profile);
        this.profileForm.markAsPristine();
      }
    });

    effect(() => {
      if (!this.favoriteBlogPostIdsResource.hasValue()) {
        return;
      }

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
      if (!this.favoriteBlogPostsResource.hasValue()) {
        return;
      }

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
        this.snackBar.open('Αποτυχία ενημέρωσης προφίλ', 'OK', { duration: 3000 });
        return;
      }

      if (!this.updateUserResource.hasValue()) {
        return;
      }

      const updatedProfile = this.updateUserResource.value();
      if (updatedProfile) {
        this.latestUpdatedProfile.set(updatedProfile);
        this.profileForm.patchValue(updatedProfile);
        this.snackBar.open('Το προφίλ ενημερώθηκε επιτυχώς', 'OK', { duration: 3000 });
        this.profileForm.markAsPristine();
        this.clearPasswordFields();
        this.updateRequest.set(null);
        this.userProfile.reload();
      }
    });

    effect(() => {
      const error = this.favoriteBlogPostsResource.error();
      if (error) {
        this.snackBar.open('Δεν ήταν δυνατή η φόρτωση των αγαπημένων αναρτήσεων.', 'OK', { duration: 3000 });
      }
    });

    effect(() => {
      const error = this.removeFavoriteResource.error();
      if (error) {
        this.snackBar.open('Αποτυχία αφαίρεσης από τα αγαπημένα.', 'OK', { duration: 3000 });
        this.favoriteRemovalRequest.set(null);
        return;
      }

      if (!this.removeFavoriteResource.hasValue()) {
        return;
      }

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
      this.favoriteBlogPostIdsResource.reload();
      this.favoriteBlogPostsResource.reload();
    });
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
        password: ['', [Validators.minLength(8), Validators.maxLength(128)]],
        confirmPassword: [''],
      },
      { validators: [this.passwordsMatchValidator()] },
    );
  }

  private passwordsMatchValidator(): ValidatorFn {
    return (control: AbstractControl): ValidationErrors | null => {
      const password = control.get('password')?.value as string | undefined;
      const confirmPassword = control.get('confirmPassword')?.value as string | undefined;

      if (!password && !confirmPassword) {
        return null;
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
    if (!error) {
      return '';
    }

    if (error instanceof HttpErrorResponse) {
      if (error.status === 0) {
        return 'Αδυναμία σύνδεσης με τον διακομιστή. Ελέγξτε τη σύνδεσή σας και δοκιμάστε ξανά.';
      }

      if (error.status === 401) {
        return 'Η συνεδρία σας έχει λήξει. Αποσυνδεθείτε και συνδεθείτε ξανά.';
      }

      if (error.status === 403) {
        return 'Δεν έχετε δικαίωμα πρόσβασης σε αυτό το προφίλ.';
      }

      if (error.status === 404) {
        return 'Το προφίλ χρήστη δε βρέθηκε.';
      }
    }

    return 'Παρουσιάστηκε σφάλμα κατά τη φόρτωση του προφίλ. Δοκιμάστε ξανά.';
  }

  onRetryLoadProfile() {
    this.userProfile.reload();
    this.favoriteBlogPostIdsResource.reload();
    this.favoriteBlogPostsResource.reload();
  }

  onDownload(url: string | undefined) {
    if (!url) return;

    const downloadURL = new URL(url);
    const anchor = document.createElement('a');
    anchor.href = downloadURL.href;
    anchor.download = downloadURL.pathname.split('/').pop() || '';
    anchor.click();
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
