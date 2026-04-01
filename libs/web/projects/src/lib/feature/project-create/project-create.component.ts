import { HttpErrorResponse } from '@angular/common/http';
import { ChangeDetectionStrategy, Component, effect, inject, signal } from '@angular/core';
import { FormArray, FormControl, FormGroup, Validators } from '@angular/forms';
import { finalize } from 'rxjs';
import { Router } from '@angular/router';

import { MatButton } from '@angular/material/button';
import { MatCard, MatCardActions, MatCardContent, MatCardHeader } from '@angular/material/card';
import { MatDivider } from '@angular/material/divider';
import { MatIcon } from '@angular/material/icon';
import { MatSnackBar } from '@angular/material/snack-bar';

import { MediaService } from '@web/shared';
import { batchMagnetUrlValidator, batchTorrentUrlValidator } from '../../data-access/batch-link.validators';
import type { BatchDownloadLinkFormModel, ProjectFormModel } from '../../data-access/project-form.interface';
import type { Project, ProjectBatchDownloadLink } from '../../data-access/project.interface';
import { ProjectsService } from '../../data-access/projects.service';
import { ProjectItemFormComponent } from '../../ui/project-item-form/project-item-form.component';

const MAX_FILE_SIZE = 5 * 1024 * 1024;

@Component({
  selector: 'sf-project-create',
  templateUrl: './project-create.component.html',
  styleUrl: './project-create.component.scss',
  imports: [
    ProjectItemFormComponent,
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
export default class ProjectCreateComponent {
  private readonly projectsService = inject(ProjectsService);
  private readonly mediaService = inject(MediaService);
  private readonly router = inject(Router);
  private readonly snackBar = inject(MatSnackBar);

  private readonly project = signal<Project | null>(null);
  private readonly thumbnailUploadInProgress = signal(false);

  protected readonly createProject = this.projectsService.createProject(this.project);

  protected readonly isThumbnailUploadInProgress = this.thumbnailUploadInProgress.asReadonly();

  protected readonly createForm = new FormGroup<ProjectFormModel>({
    title: new FormControl('', {
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
    batchDownloadLinks: new FormArray<FormGroup<BatchDownloadLinkFormModel>>([this.createBatchDownloadLinkGroup()]),
  });

  constructor() {
    effect(() => {
      if (this.createProject.value()) {
        this.router.navigate(['/', 'projects'], { replaceUrl: true });
      }
    });
  }

  get formControl() {
    return this.createForm.controls;
  }

  get batchDownloadLinks() {
    return this.createForm.controls['batchDownloadLinks'] as FormArray<FormGroup<BatchDownloadLinkFormModel>>;
  }

  private createBatchDownloadLinkGroup(): FormGroup<BatchDownloadLinkFormModel> {
    return new FormGroup<BatchDownloadLinkFormModel>({
      name: new FormControl('', {
        nonNullable: true,
        validators: [Validators.required],
      }),
      downloadLinkTorrent: new FormControl('', {
        nonNullable: true,
        validators: [Validators.required, batchTorrentUrlValidator()],
      }),
      downloadLink: new FormControl('', {
        nonNullable: true,
        validators: [Validators.required, batchMagnetUrlValidator()],
      }),
      downloadLink4kTorrent: new FormControl('', {
        nonNullable: true,
        validators: [batchTorrentUrlValidator()],
      }),
      downloadLink4k: new FormControl('', {
        nonNullable: true,
        validators: [batchMagnetUrlValidator()],
      }),
    });
  }

  onSubmit() {
    if (this.createForm.valid) {
      const projectData: Project = {
        title: this.createForm.value.title ?? '',
        description: this.createForm.value.description ?? '',
        thumbnail: this.createForm.value.thumbnail ?? '',
        batchDownloadLinks: this.batchDownloadLinks.controls
          .map((control) => ({
            name: String(control.controls.name.value ?? '').trim(),
            downloadLinkTorrent: String(control.controls.downloadLinkTorrent.value ?? '').trim(),
            downloadLink: String(control.controls.downloadLink.value ?? '').trim(),
            downloadLink4kTorrent: String(control.controls.downloadLink4kTorrent.value ?? '').trim() || undefined,
            downloadLink4k: String(control.controls.downloadLink4k.value ?? '').trim() || undefined,
          }))
          .filter(
            (link) => link.name.length > 0 && link.downloadLinkTorrent.length > 0 && link.downloadLink.length > 0,
          ) as ProjectBatchDownloadLink[],
        dateTimeCreated: new Date(),
      } as Project;

      this.project.set(projectData);
    }
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
