import { Component, OnInit } from '@angular/core';
import { FormControl, Validators, FormArray, FormBuilder } from '@angular/forms';
import { ActivatedRoute } from '@angular/router';
import { map } from 'rxjs';

import { ProjectsService } from '../../data-access/projects.service';
import { EditProjectForm } from '../../data-access/edit-project.interface';
import { Project } from '../../data-access/project.interface';

@Component({
  selector: 'sick-project-detail-edit',
  templateUrl: './project-detail-edit.component.html',
  styleUrl: './project-detail-edit.component.scss',
})
export class ProjectDetailEditComponent implements OnInit {
  private id = '';

  editForm = this.fb.group<EditProjectForm>({
    title: new FormControl('', { nonNullable: true, validators: [Validators.required] }),
    description: new FormControl('', { nonNullable: true, validators: [Validators.required] }),
    thumbnail: new FormControl('', { nonNullable: true, validators: [Validators.required] }),
    batchDownloadLinks: this.fb.array([]),
  });

  get formControl() {
    return this.editForm.controls;
  }

  get batchDownloadLinks() {
    return this.editForm.controls['batchDownloadLinks'] as FormArray;
  }

  constructor(
    private readonly projectsService: ProjectsService,
    private readonly route: ActivatedRoute,
    private readonly fb: FormBuilder,
  ) {}

  ngOnInit(): void {
    this.id = this.route.snapshot.paramMap.get('id') ?? '';

    if (this.id !== '') {
      this.projectsService
        .getProjectById(this.id)
        .pipe(
          map((project) => ({
            title: project.title,
            description: project.description,
            thumbnail: project.thumbnail,
            batchDownloadLinks: project.batchDownloadLinks,
          })),
        )
        .subscribe((project) => {
          this.editForm.get('title')?.setValue(project.title);
          this.editForm.get('description')?.setValue(project.description);
          this.editForm.get('thumbnail')?.setValue(project.thumbnail);

          project.batchDownloadLinks?.forEach((link) => {
            this.batchDownloadLinks.push(new FormControl(link));
          });
        });
    }
  }

  onAddBatchDownloadLink() {
    this.batchDownloadLinks.push(new FormControl(''));
  }

  onDeleteBatchDownloadLink(index: number) {
    this.batchDownloadLinks.removeAt(index);
  }

  onSave() {
    this.projectsService.updateProject(this.id, this.editForm.value as Project);
  }
}
