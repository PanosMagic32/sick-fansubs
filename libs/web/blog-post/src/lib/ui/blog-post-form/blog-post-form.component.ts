import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';
import { type FormGroup, ReactiveFormsModule } from '@angular/forms';
import { MatButton } from '@angular/material/button';
import { MatError, MatFormField, MatLabel } from '@angular/material/form-field';
import { MatIcon } from '@angular/material/icon';
import { MatInput } from '@angular/material/input';

import type { PostFormModel } from '../../data-access/post-form.interface';

@Component({
  selector: 'sf-blog-post-form',
  templateUrl: './blog-post-form.component.html',
  styleUrl: './blog-post-form.component.scss',
  imports: [ReactiveFormsModule, MatFormField, MatLabel, MatError, MatInput, MatButton, MatIcon],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class BlogPostFormComponent {
  readonly form = input.required<FormGroup<PostFormModel>>();
  readonly isUploadingThumbnail = input(false);

  readonly thumbnailFileSelected = output<File>();

  protected selectedThumbnailFileName: string | null = null;
  protected thumbnailPreviewLoadError = false;
  private lastPreviewSource = '';

  get formControls() {
    return this.form().controls;
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
