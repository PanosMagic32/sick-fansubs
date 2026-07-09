import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';
import {
  AbstractControl,
  type FormArray,
  FormControl,
  FormGroup,
  ReactiveFormsModule,
  type ValidationErrors,
  Validators,
} from '@angular/forms';

import { MatButton, MatMiniFabButton } from '@angular/material/button';
import { MatError, MatFormField, MatLabel } from '@angular/material/form-field';
import { MatIcon } from '@angular/material/icon';
import { MatInput } from '@angular/material/input';

import { batchMagnetUrlValidator, batchTorrentUrlValidator } from '../../data-access/batch-link.validators';
import type { BatchDownloadLinkFormModel, ProjectFormModel } from '../../data-access/project-form.interface';

function atLeastOneResolutionForBatch(form: AbstractControl): ValidationErrors | null {
  const dl = String(form.get('downloadLink')?.value ?? '').trim();
  const dlTorrent = String(form.get('downloadLinkTorrent')?.value ?? '').trim();
  const dl4k = String(form.get('downloadLink4k')?.value ?? '').trim();
  const dl4kTorrent = String(form.get('downloadLink4kTorrent')?.value ?? '').trim();

  const has1080p = dl.length > 0 && dlTorrent.length > 0;
  const has2160p = dl4k.length > 0 && dl4kTorrent.length > 0;

  return has1080p || has2160p ? null : { atLeastOneResolution: true };
}

@Component({
  selector: 'sf-project-item-form',
  templateUrl: './project-item-form.component.html',
  styleUrl: './project-item-form.component.scss',
  imports: [ReactiveFormsModule, MatFormField, MatLabel, MatError, MatInput, MatIcon, MatMiniFabButton, MatButton],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ProjectItemFormComponent {
  readonly form = input.required<FormGroup<ProjectFormModel>>();
  readonly isUploadingThumbnail = input(false);

  readonly thumbnailFileSelected = output<File>();

  protected selectedThumbnailFileName: string | null = null;
  protected thumbnailPreviewLoadError = false;
  private lastPreviewSource = '';

  get formControls() {
    return this.form().controls;
  }

  get batchDownloadLinks() {
    return this.form().controls['batchDownloadLinks'] as FormArray<FormGroup<BatchDownloadLinkFormModel>>;
  }

  get thumbnailPreviewUrl(): string | null {
    const source = String(this.formControls.thumbnail.value ?? '').trim();
    if (!source) {
      this.thumbnailPreviewLoadError = false;
      this.lastPreviewSource = '';
      return null;
    }

    if (source !== this.lastPreviewSource) {
      this.thumbnailPreviewLoadError = false;
      this.lastPreviewSource = source;
    }

    if (this.thumbnailPreviewLoadError) {
      return null;
    }

    try {
      const parsedUrl = new URL(source, window.location.origin);
      if (parsedUrl.protocol !== 'http:' && parsedUrl.protocol !== 'https:') {
        return null;
      }

      return parsedUrl.href;
    } catch {
      return null;
    }
  }

  onAddBatchDownloadLink() {
    this.batchDownloadLinks.push(
      new FormGroup<BatchDownloadLinkFormModel>(
        {
          name: new FormControl('', { nonNullable: true, validators: [Validators.required] }),
          downloadLinkTorrent: new FormControl('', {
            nonNullable: true,
            validators: [batchTorrentUrlValidator()],
          }),
          downloadLink: new FormControl('', {
            nonNullable: true,
            validators: [batchMagnetUrlValidator()],
          }),
          downloadLink4kTorrent: new FormControl('', {
            nonNullable: true,
            validators: [batchTorrentUrlValidator()],
          }),
          downloadLink4k: new FormControl('', {
            nonNullable: true,
            validators: [batchMagnetUrlValidator()],
          }),
        },
        { validators: [atLeastOneResolutionForBatch] },
      ),
    );
  }

  onDeleteBatchDownloadLink(index: number) {
    this.batchDownloadLinks.removeAt(index);
  }

  onThumbnailFileSelected(event: Event): void {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];

    if (file) {
      this.selectedThumbnailFileName = file.name;
      this.thumbnailFileSelected.emit(file);
    } else {
      this.selectedThumbnailFileName = null;
    }

    input.value = '';
  }

  onThumbnailPreviewError(): void {
    this.thumbnailPreviewLoadError = true;
  }
}
