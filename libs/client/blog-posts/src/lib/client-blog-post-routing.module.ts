import { NgModule } from '@angular/core';
import { RouterModule, Routes } from '@angular/router';

import { AuthGuard } from '@sick/client/auth';

import { PostListComponent } from './feature/post-list/post-list.component';
import { PostDetailComponent } from './feature/post-detail/post-detail.component';
import { PostCreateComponent } from './feature/post-create/post-create.component';

const routes: Routes = [
  {
    path: '',
    component: PostListComponent,
  },
  {
    path: 'create',
    canActivate: [AuthGuard],
    component: PostCreateComponent,
  },
  {
    path: ':id/edit',
    canActivate: [AuthGuard],
    component: PostDetailComponent,
  },
];

@NgModule({
  imports: [RouterModule.forChild(routes)],
  exports: [RouterModule],
})
export class ClientBlogPostsRoutingModule {}
