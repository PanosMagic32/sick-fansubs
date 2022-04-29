import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';

import { MaterialModule } from '@sick/material';

import { ClientProjectsRoutingModule } from './client-projects-routing.module';
import { ProjectListComponent } from './feature/project-list/projects-list.component';
import { ProjectDetailComponent } from './feature/project-detail/project-detail.component';
import { ProjectItemComponent } from './ui/project-item/project-item.component';

@NgModule({
  imports: [CommonModule, ClientProjectsRoutingModule, MaterialModule],
  declarations: [ProjectListComponent, ProjectDetailComponent, ProjectItemComponent],
  exports: [ProjectListComponent, ProjectDetailComponent, ProjectItemComponent],
})
export class ClientProjectsModule {}
