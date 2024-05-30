import { NgModule } from '@angular/core';
import { CommonModule, NgOptimizedImage } from '@angular/common';
import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';
import { ReactiveFormsModule } from '@angular/forms';
import { MaterialModule } from '@sick/material';
import { SharedModule } from '@sick/shared';

import { ClientBlogPostsRoutingModule } from './client-blog-post-routing.module';
import { PostListComponent } from './feature/post-list/post-list.component';
import { PostDetailComponent } from './feature/post-detail/post-detail.component';
import { PostCreateComponent } from './feature/post-create/post-create.component';
import { PostItemComponent } from './ui/post-item/post-item.component';

@NgModule({
  declarations: [PostListComponent, PostItemComponent, PostDetailComponent, PostCreateComponent],
  exports: [PostListComponent],
  imports: [CommonModule, ClientBlogPostsRoutingModule, MaterialModule, SharedModule, NgOptimizedImage, ReactiveFormsModule],
  providers: [provideHttpClient(withInterceptorsFromDi())],
})
export class ClientBlogPostsModule {}
