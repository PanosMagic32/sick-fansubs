import { NgModule } from '@angular/core';
import { CommonModule, NgOptimizedImage } from '@angular/common';
import { ReactiveFormsModule } from '@angular/forms';

import { MaterialModule } from '@sick/material';

import { SharedModule } from '@sick/shared';

import { ClientProjectsRoutingModule } from './client-projects-routing.module';
import { ProjectListComponent } from './feature/project-list/projects-list.component';
import { ProjectDetailComponent } from './feature/project-detail/project-detail.component';
import { ProjectDetailEditComponent } from './feature/project-detail-edit/project-detail-edit.component';
import { ProjectCreateComponent } from './feature/project-create/project-create.component';
import { ProjectItemComponent } from './ui/project-item/project-item.component';

@NgModule({
  imports: [CommonModule, ClientProjectsRoutingModule, MaterialModule, SharedModule, NgOptimizedImage, ReactiveFormsModule],
  declarations: [
    ProjectListComponent,
    ProjectDetailComponent,
    ProjectItemComponent,
    ProjectDetailEditComponent,
    ProjectCreateComponent,
  ],
  exports: [ProjectListComponent, ProjectDetailComponent, ProjectItemComponent],
})
export class ClientProjectsModule {}
