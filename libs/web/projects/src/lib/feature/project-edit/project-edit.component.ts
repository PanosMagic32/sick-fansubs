import { ChangeDetectionStrategy, Component, effect, inject, input, signal } from '@angular/core';
import { FormArray, FormControl, FormGroup, Validators } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';

import { MatButton } from '@angular/material/button';
import { MatCard, MatCardActions, MatCardContent, MatCardHeader } from '@angular/material/card';
import { MatDivider } from '@angular/material/divider';
import { MatIcon } from '@angular/material/icon';

import type { ProjectFormModel } from '../../data-access/project-form.interface';
import type { Project } from '../../data-access/project.interface';
import { ProjectsService } from '../../data-access/projects.service';
import { ProjectItemFormComponent } from '../../ui/project-item-form/project-item-form.component';

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

  readonly id = input.required<string>();

  private readonly updateRequest = signal<Project | null>(null);

  protected readonly selectedProject = this.projectsService.getProjectById(this.id);
  protected readonly updateResource = this.projectsService.updateProject(this.id, this.updateRequest);
  protected readonly deleteResource = signal<boolean>(false);

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
    batchDownloadLinks: new FormArray([
      new FormControl('', {
        nonNullable: true,
        validators: [Validators.required],
      }),
    ]),
  });

  get formControl() {
    return this.editForm.controls;
  }

  get batchDownloadLinksControls() {
    return this.editForm.controls['batchDownloadLinks'] as FormArray;
  }

  constructor() {
    effect(() => {
      const project = this.selectedProject.value();
      if (project) {
        this.editForm.get('title')?.setValue(project.title ?? '');
        this.editForm.get('description')?.setValue(project.description ?? '');
        this.editForm.get('thumbnail')?.setValue(project.thumbnail ?? '');

        this.batchDownloadLinksControls.clear();

        project.batchDownloadLinks?.forEach((link) => {
          this.batchDownloadLinksControls.push(new FormControl(link));
        });
      }
    });

    effect(() => {
      if (this.updateResource.value() || this.deleteResource()) {
        this.router.navigate(['../..'], {
          replaceUrl: true,
          relativeTo: this.activatedRoute,
        });
      }
    });
  }

  onSave() {
    if (this.editForm.valid) {
      this.updateRequest.set(this.editForm.getRawValue() as Project);
    }
  }

  onDelete() {
    this.deleteResource.set(true);
  }

  onCancel() {
    this.router.navigate(['../..'], {
      replaceUrl: true,
      relativeTo: this.activatedRoute,
    });
  }
}
