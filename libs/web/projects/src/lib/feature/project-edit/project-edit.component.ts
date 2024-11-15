import { ChangeDetectionStrategy, Component, computed, effect, inject, input, type OnInit } from '@angular/core';
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
  standalone: true,
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
export default class ProjectEditComponent implements OnInit {
  private readonly router = inject(Router);
  private readonly activatedRoute = inject(ActivatedRoute);
  private readonly projectsService = inject(ProjectsService);

  readonly id = input.required<string>();

  protected readonly selectedProject = computed(() => this.projectsService.selectedProject());

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
      this.editForm.get('title')?.setValue(this.selectedProject()?.title ?? '');
      this.editForm.get('description')?.setValue(this.selectedProject()?.description ?? '');
      this.editForm.get('thumbnail')?.setValue(this.selectedProject()?.thumbnail ?? '');

      this.batchDownloadLinksControls.clear();

      this.selectedProject()?.batchDownloadLinks?.forEach((link) => {
        this.batchDownloadLinksControls.push(new FormControl(link));
      });
    });
  }

  ngOnInit() {
    if (this.id() !== '') {
      this.projectsService.getProjectById(this.id());
    }
  }

  onSave() {
    this.projectsService.updateProject(this.id(), this.editForm.value as Project);
  }

  onDelete() {
    this.projectsService.deleteProject(this.id());
  }

  onCancel() {
    this.router.navigate(['../..'], {
      replaceUrl: true,
      relativeTo: this.activatedRoute,
    });
  }
}
