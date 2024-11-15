import { ChangeDetectionStrategy, Component, input } from '@angular/core';
import { type FormArray, FormControl, type FormGroup, ReactiveFormsModule } from '@angular/forms';

import { MatButton, MatMiniFabButton } from '@angular/material/button';
import { MatError, MatFormField, MatLabel } from '@angular/material/form-field';
import { MatIcon } from '@angular/material/icon';
import { MatInput } from '@angular/material/input';

import type { ProjectFormModel } from '../../data-access/project-form.interface';

@Component({
  selector: 'sf-project-item-form',
  templateUrl: './project-item-form.component.html',
  styleUrl: './project-item-form.component.scss',
  standalone: true,
  imports: [ReactiveFormsModule, MatFormField, MatLabel, MatError, MatInput, MatIcon, MatMiniFabButton, MatButton],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ProjectItemFormComponent {
  readonly form = input.required<FormGroup<ProjectFormModel>>();

  get formControls() {
    return this.form().controls;
  }

  get batchDownloadLinks() {
    return this.form().controls['batchDownloadLinks'] as FormArray;
  }

  onAddBatchDownloadLink() {
    this.batchDownloadLinks.push(new FormControl(''));
  }

  onDeleteBatchDownloadLink(index: number) {
    this.batchDownloadLinks.removeAt(index);
  }
}
