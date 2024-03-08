import { ChangeDetectionStrategy, Component } from '@angular/core';
import { FormControl, Validators, FormArray, FormBuilder } from '@angular/forms';

import { ProjectForm } from '../../data-access/project-form.interface';
import { ProjectsService } from '../../data-access/projects.service';
import { Project } from '../../data-access/project.interface';

@Component({
  selector: 'sick-project-create',
  templateUrl: './project-create.component.html',
  styleUrl: './project-create.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ProjectCreateComponent {
  createForm = this.fb.group<ProjectForm>({
    title: new FormControl('', { nonNullable: true, validators: [Validators.required] }),
    description: new FormControl('', { nonNullable: true, validators: [Validators.required] }),
    thumbnail: new FormControl('', { nonNullable: true, validators: [Validators.required] }),
    batchDownloadLinks: this.fb.array([new FormControl('', { nonNullable: true, validators: [Validators.required] })]),
  });

  get formControl() {
    return this.createForm.controls;
  }

  get batchDownloadLinks() {
    return this.createForm.controls['batchDownloadLinks'] as FormArray;
  }

  constructor(
    private readonly projectsService: ProjectsService,
    private readonly fb: FormBuilder,
  ) {}

  onAddBatchDownloadLink() {
    this.batchDownloadLinks.push(new FormControl(''));
  }

  onDeleteBatchDownloadLink(index: number) {
    this.batchDownloadLinks.removeAt(index);
  }

  onSubmit() {
    if (this.createForm.valid) {
      const project = {
        ...this.createForm.value,
        dateTimeCreated: new Date(),
      } as Project;

      this.projectsService.createProject(project);
    }
  }
}
