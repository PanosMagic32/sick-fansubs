import { NgModule } from '@angular/core';
import { RouterModule, Routes } from '@angular/router';

import { AuthGuard } from '@sick/client/auth';

import { ProjectListComponent } from './feature/project-list/projects-list.component';
import { ProjectDetailComponent } from './feature/project-detail/project-detail.component';
import { ProjectDetailEditComponent } from './feature/project-detail-edit/project-detail-edit.component';
import { ProjectCreateComponent } from './feature/project-create/project-create.component';

const routes: Routes = [
  {
    path: '',
    component: ProjectListComponent,
  },
  {
    path: 'create',
    canActivate: [AuthGuard],
    component: ProjectCreateComponent,
  },
  {
    path: ':id',
    component: ProjectDetailComponent,
  },
  {
    path: ':id/edit',
    canActivate: [AuthGuard],
    component: ProjectDetailEditComponent,
  },
];

@NgModule({
  imports: [RouterModule.forChild(routes)],
  exports: [RouterModule],
})
export class ClientProjectsRoutingModule {}
