import { HttpErrorResponse } from '@angular/common/http';
import { ChangeDetectionStrategy, Component, effect, inject, input, signal } from '@angular/core';
import { FormArray, FormControl, FormGroup, Validators } from '@angular/forms';
import { finalize } from 'rxjs';
import { ActivatedRoute, Router } from '@angular/router';

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
  selector: 'sf-project-edit',
  templateUrl: './project-edit.component.html',
  styleUrl: './project-edit.component.scss',
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
export default class ProjectEditComponent {
  private readonly router = inject(Router);
  private readonly activatedRoute = inject(ActivatedRoute);
  private readonly projectsService = inject(ProjectsService);
  private readonly mediaService = inject(MediaService);
  private readonly snackBar = inject(MatSnackBar);

  readonly id = input.required<string>();

  private readonly updateRequest = signal<Project | null>(null);
  private readonly deleteRequest = signal<string | null>(null);
  private readonly thumbnailUploadInProgress = signal(false);

  protected readonly selectedProject = this.projectsService.getProjectById(this.id);
  protected readonly updateResource = this.projectsService.updateProject(this.id, this.updateRequest);
  protected readonly deleteResource = this.projectsService.deleteProject(this.deleteRequest);

  protected readonly isThumbnailUploadInProgress = this.thumbnailUploadInProgress.asReadonly();

  protected readonly editForm = new FormGroup<ProjectFormModel>({
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

  get formControl() {
    return this.editForm.controls;
  }

  get batchDownloadLinksControls() {
    return this.editForm.controls['batchDownloadLinks'] as FormArray<FormGroup<BatchDownloadLinkFormModel>>;
  }

  private createBatchDownloadLinkGroup(
    name = '',
    downloadLinkTorrent = '',
    downloadLink = '',
    downloadLink4kTorrent = '',
    downloadLink4k = '',
  ): FormGroup<BatchDownloadLinkFormModel> {
    return new FormGroup<BatchDownloadLinkFormModel>({
      name: new FormControl(name, {
        nonNullable: true,
        validators: [Validators.required],
      }),
      downloadLinkTorrent: new FormControl(downloadLinkTorrent, {
        nonNullable: true,
        validators: [Validators.required, batchTorrentUrlValidator()],
      }),
      downloadLink: new FormControl(downloadLink, {
        nonNullable: true,
        validators: [Validators.required, batchMagnetUrlValidator()],
      }),
      downloadLink4kTorrent: new FormControl(downloadLink4kTorrent, {
        nonNullable: true,
        validators: [batchTorrentUrlValidator()],
      }),
      downloadLink4k: new FormControl(downloadLink4k, {
        nonNullable: true,
        validators: [batchMagnetUrlValidator()],
      }),
    });
  }

  private normalizeBatchLink(
    link: ProjectBatchDownloadLink | string,
    index: number,
  ): {
    name: string;
    downloadLinkTorrent: string;
    downloadLink: string;
    downloadLink4kTorrent: string;
    downloadLink4k: string;
  } {
    if (typeof link === 'string') {
      return {
        name: `Batch ${index + 1}`,
        downloadLinkTorrent: /^https?:\/\//i.test(link) ? link : '',
        downloadLink: /^magnet:\?xt=/i.test(link) ? link : '',
        downloadLink4kTorrent: '',
        downloadLink4k: '',
      };
    }

    const legacySingleUrl = (link as { url?: unknown }).url;
    const legacyUrlValue = typeof legacySingleUrl === 'string' ? legacySingleUrl.trim() : '';

    const normalizedTorrent = String((link as { downloadLinkTorrent?: unknown }).downloadLinkTorrent ?? '').trim();
    const normalizedMagnet = String((link as { downloadLink?: unknown }).downloadLink ?? '').trim();
    const normalizedTorrent4k = String((link as { downloadLink4kTorrent?: unknown }).downloadLink4kTorrent ?? '').trim();
    const normalizedMagnet4k = String((link as { downloadLink4k?: unknown }).downloadLink4k ?? '').trim();

    return {
      name: String(link?.name ?? '').trim(),
      downloadLinkTorrent: normalizedTorrent || (/^https?:\/\//i.test(legacyUrlValue) ? legacyUrlValue : ''),
      downloadLink: normalizedMagnet || (/^magnet:\?xt=/i.test(legacyUrlValue) ? legacyUrlValue : ''),
      downloadLink4kTorrent: normalizedTorrent4k,
      downloadLink4k: normalizedMagnet4k,
    };
  }

  constructor() {
    effect(() => {
      const project = this.selectedProject.value();
      if (project) {
        this.editForm.get('title')?.setValue(project.title ?? '');
        this.editForm.get('description')?.setValue(project.description ?? '');
        this.editForm.get('thumbnail')?.setValue(project.thumbnail ?? '');

        this.batchDownloadLinksControls.clear();

        project.batchDownloadLinks?.forEach((link, index) => {
          const normalizedLink = this.normalizeBatchLink(link as ProjectBatchDownloadLink | string, index);
          this.batchDownloadLinksControls.push(
            this.createBatchDownloadLinkGroup(
              normalizedLink.name,
              normalizedLink.downloadLinkTorrent,
              normalizedLink.downloadLink,
              normalizedLink.downloadLink4kTorrent,
              normalizedLink.downloadLink4k,
            ),
          );
        });

        if (this.batchDownloadLinksControls.length === 0) {
          this.batchDownloadLinksControls.push(this.createBatchDownloadLinkGroup());
        }
      }
    });

    effect(() => {
      if (this.updateResource.value() || this.deleteResource.value()) {
        this.router.navigate(['../..'], {
          replaceUrl: true,
          relativeTo: this.activatedRoute,
        });
      }
    });
  }

  onSave() {
    if (this.editForm.valid) {
      const formValue = this.editForm.getRawValue();
      this.updateRequest.set({
        ...formValue,
        batchDownloadLinks: this.batchDownloadLinksControls.controls
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
      } as Project);
    }
  }

  onDelete() {
    this.deleteRequest.set(this.id());
  }

  onCancel() {
    this.router.navigate(['../..'], {
      replaceUrl: true,
      relativeTo: this.activatedRoute,
    });
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
