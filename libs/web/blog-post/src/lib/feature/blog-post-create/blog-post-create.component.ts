import { HttpErrorResponse } from '@angular/common/http';
import { ChangeDetectionStrategy, Component, effect, inject, signal } from '@angular/core';
import { FormControl, FormGroup, Validators } from '@angular/forms';
import { finalize } from 'rxjs';

import { MatButton } from '@angular/material/button';
import { MatCard, MatCardActions, MatCardContent, MatCardHeader } from '@angular/material/card';
import { MatDivider } from '@angular/material/divider';
import { MatIcon } from '@angular/material/icon';
import { MatProgressBar } from '@angular/material/progress-bar';
import { MatSnackBar } from '@angular/material/snack-bar';

import { MediaService, NoContentComponent } from '@web/shared';

import { ActivatedRoute, Router } from '@angular/router';
import type { CreateBlogPost } from '../../data-access/blog-post.interface';
import { BlogPostService } from '../../data-access/blog-post.service';
import type { PostFormModel } from '../../data-access/post-form.interface';
import { BlogPostFormComponent } from '../../ui/blog-post-form/blog-post-form.component';

const MAX_FILE_SIZE = 5 * 1024 * 1024;

@Component({
  selector: 'sf-blog-post-create',
  templateUrl: './blog-post-create.component.html',
  styleUrl: './blog-post-create.component.scss',
  imports: [
    BlogPostFormComponent,
    NoContentComponent,
    MatProgressBar,
    MatCard,
    MatCardHeader,
    MatCardContent,
    MatCardActions,
    MatIcon,
    MatButton,
    MatDivider,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export default class BlogPostCreateComponent {
  private readonly blogPostService = inject(BlogPostService);
  private readonly mediaService = inject(MediaService);
  private readonly router = inject(Router);
  private readonly activatedRoute = inject(ActivatedRoute);
  private readonly snackBar = inject(MatSnackBar);

  private readonly blogPost = signal<CreateBlogPost | null>(null);
  private readonly thumbnailUploadInProgress = signal(false);

  protected readonly createBlogPost = this.blogPostService.createBlogPost(this.blogPost);
  protected readonly isThumbnailUploadInProgress = this.thumbnailUploadInProgress.asReadonly();

  protected readonly createForm = new FormGroup<PostFormModel>({
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
      if (this.createBlogPost.hasValue() && !this.createBlogPost.error() && !this.createBlogPost.isLoading()) {
        this.router.navigate(['/'], { relativeTo: this.activatedRoute });
      }
    });
  }

  onSubmit() {
    if (this.createForm.invalid) return;

    const post: CreateBlogPost = {
      title: this.createForm.value.title ?? '',
      subtitle: this.createForm.value.subtitle ?? '',
      description: this.createForm.value.description ?? '',
      thumbnail: this.createForm.value.thumbnail ?? '',
      downloadLinkTorrent: this.createForm.value.downloadLinkTorrent ?? '',
      downloadLink: this.createForm.value.downloadLink ?? '',
      downloadLink4kTorrent: this.createForm.value.downloadLink4kTorrent ?? '',
      downloadLink4k: this.createForm.value.downloadLink4k ?? '',
      dateTimeCreated: new Date(),
    };

    this.blogPost.set(post);
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
          this.createForm.patchValue({ thumbnail: url });
          this.createForm.get('thumbnail')?.markAsDirty();
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
