import { NgModule } from '@angular/core';
import { RouterModule, Routes } from '@angular/router';

import { AuthGuard } from '@sick/client/auth';

import { PostListComponent } from './feature/post-list/post-list.component';
import { PostDetailComponent } from './feature/post-detail/post-detail.component';

const routes: Routes = [
  {
    path: '',
    component: PostListComponent,
  },
  {
    path: ':id',
    canActivate: [AuthGuard],
    component: PostDetailComponent,
  },
];

@NgModule({
  imports: [RouterModule.forChild(routes)],
  exports: [RouterModule],
})
export class ClientBlogPostsRoutingModule {}
