import { NgModule } from '@angular/core';
import { RouterModule, Routes } from '@angular/router';

import { PostListComponent } from '@sick/client/home';
import { ProjectListComponent } from '@sick/client/projects';

const routes: Routes = [
  {
    path: 'home',
    component: PostListComponent,
  },
  {
    path: 'projects',
    component: ProjectListComponent,
  },
];

@NgModule({
  imports: [RouterModule.forChild(routes)],
  exports: [RouterModule],
})
export class ClientShellRoutingModule {}
