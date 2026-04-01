import { HttpErrorResponse } from '@angular/common/http';
import { ChangeDetectionStrategy, Component, effect, inject, input, signal } from '@angular/core';
import { FormControl, FormGroup, Validators } from '@angular/forms';
import { finalize } from 'rxjs';
import { Router } from '@angular/router';

import { MatButton } from '@angular/material/button';
import { MatCard, MatCardActions, MatCardContent, MatCardHeader } from '@angular/material/card';
import { MatDivider } from '@angular/material/divider';
import { MatIcon } from '@angular/material/icon';
import { MatSnackBar } from '@angular/material/snack-bar';

import { MediaService } from '@web/shared';
import type { EditBlogPost } from '../../data-access/blog-post.interface';
import { BlogPostService } from '../../data-access/blog-post.service';
import type { PostFormModel } from '../../data-access/post-form.interface';
import { BlogPostFormComponent } from '../../ui/blog-post-form/blog-post-form.component';

const MAX_FILE_SIZE = 5 * 1024 * 1024;

@Component({
  selector: 'sf-blog-post-edit',
  templateUrl: './blog-post-edit.component.html',
  styleUrl: './blog-post-edit.component.scss',
  imports: [BlogPostFormComponent, MatCard, MatCardHeader, MatCardContent, MatCardActions, MatIcon, MatButton, MatDivider],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export default class BlogPostEditComponent {
  private readonly router = inject(Router);
  private readonly blogPostService = inject(BlogPostService);
  private readonly mediaService = inject(MediaService);
  private readonly snackBar = inject(MatSnackBar);

  readonly id = input.required<string>();

  private readonly updateRequest = signal<EditBlogPost | null>(null);
  private readonly deleteRequest = signal<string | null>(null);
  private readonly thumbnailUploadInProgress = signal(false);

  protected readonly blogPost = this.blogPostService.getBlogPostById(this.id);
  protected readonly updateResource = this.blogPostService.updateBlogPost(this.id, this.updateRequest);
  protected readonly deleteResource = this.blogPostService.deleteBlogPost(this.deleteRequest);

  protected readonly isThumbnailUploadInProgress = this.thumbnailUploadInProgress.asReadonly();

  protected readonly editForm = new FormGroup<PostFormModel>({
    title: new FormControl('', {
      nonNullable: true,
      validators: [Validators.required],
    }),
    subtitle: new FormControl('', {
      nonNullable: true,
      validators: [Validators.required],
    }),
    description: new FormControl('', {
      nonNullable: true,
      validators: [Validators.required],
    }),
    thumbnail: new FormControl('', {
      nonNullable: true,
      validators: [Validators.required],
    }),
    downloadLinkTorrent: new FormControl('', {
      nonNullable: true,
      validators: [Validators.required],
    }),
    downloadLink: new FormControl('', {
      nonNullable: true,
      validators: [Validators.required],
    }),
    downloadLink4kTorrent: new FormControl('', {
      nonNullable: true,
    }),
    downloadLink4k: new FormControl('', {
      nonNullable: true,
    }),
  });

  constructor() {
    effect(() => {
      const post = this.blogPost.value();
      if (post) {
        this.editForm.patchValue({
          title: post.title,
          subtitle: post.subtitle,
          description: post.description,
          thumbnail: post.thumbnail,
          downloadLinkTorrent: post.downloadLinkTorrent ?? '',
          downloadLink: post.downloadLink,
          downloadLink4kTorrent: post.downloadLink4kTorrent ?? '',
          downloadLink4k: post.downloadLink4k ?? '',
        });
        this.editForm.markAsPristine();
      }
    });

    effect(() => {
      if (this.updateResource.value()) {
        this.router.navigate(['../..'], { replaceUrl: true });
      }
    });

    effect(() => {
      if (this.deleteResource.value()) {
        this.router.navigate(['../..'], { replaceUrl: true });
      }
    });
  }

  onSave() {
    if (this.editForm.valid) {
      this.updateRequest.set(this.editForm.getRawValue() as EditBlogPost);
    }
  }

  onDelete() {
    this.deleteRequest.set(this.id());
  }

  onCancel() {
    this.router.navigate(['../..'], { replaceUrl: true });
  }

  onThumbnailFileSelected(file: File): void {
    if (file.size > MAX_FILE_SIZE) {
      this.snackBar.open('Το αρχείο δεν μπορεί να υπερβαίνει τα 5 MB.', 'OK', { duration: 3000 });
      return;
    }

    this.thumbnailUploadInProgress.set(true);

    this.mediaService
      .uploadImage(file)
      .pipe(finalize(() => this.thumbnailUploadInProgress.set(false)))
      .subscribe({
        next: ({ url }) => {
          this.editForm.patchValue({ thumbnail: url });
          this.editForm.get('thumbnail')?.markAsDirty();
          this.snackBar.open('Η εικόνα ανέβηκε και το URL συμπληρώθηκε.', 'OK', { duration: 3000 });
        },
        error: (error: unknown) => {
          this.snackBar.open(this.getThumbnailUploadErrorMessage(error), 'OK', { duration: 3500 });
        },
      });
  }

  private getThumbnailUploadErrorMessage(error: unknown): string {
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
}
