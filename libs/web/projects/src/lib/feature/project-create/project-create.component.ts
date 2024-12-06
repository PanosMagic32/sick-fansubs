import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { FormArray, FormControl, FormGroup, Validators } from '@angular/forms';

import { MatButton } from '@angular/material/button';
import { MatCard, MatCardActions, MatCardContent, MatCardHeader } from '@angular/material/card';
import { MatDivider } from '@angular/material/divider';
import { MatIcon } from '@angular/material/icon';

import type { ProjectFormModel } from '../../data-access/project-form.interface';
import type { CreateProject } from '../../data-access/project.interface';
import { ProjectsService } from '../../data-access/projects.service';
import { ProjectItemFormComponent } from '../../ui/project-item-form/project-item-form.component';

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
    batchDownloadLinks: new FormArray([
      new FormControl('', {
        nonNullable: true,
        validators: [Validators.required],
      }),
    ]),
  });

  get formControl() {
    return this.createForm.controls;
  }

  get batchDownloadLinks() {
    return this.createForm.controls['batchDownloadLinks'] as FormArray;
  }

  onAddBatchDownloadLink() {
    this.batchDownloadLinks.push(new FormControl(''));
  }

  onDeleteBatchDownloadLink(index: number) {
    this.batchDownloadLinks.removeAt(index);
  }

  onSubmit() {
    if (this.createForm.valid) {
      const project: CreateProject = {
        title: this.createForm.value.title ?? '',
        description: this.createForm.value.description ?? '',
        thumbnail: this.createForm.value.thumbnail ?? '',
        dateTimeCreated: new Date(),
      };

      this.projectsService.createProject(project);
    }
  }
}
