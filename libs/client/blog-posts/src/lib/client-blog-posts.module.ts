import { NgModule } from '@angular/core';
import { CommonModule, NgOptimizedImage } from '@angular/common';
import { HttpClientModule } from '@angular/common/http';

import { MaterialModule } from '@sick/material';
import { SharedModule } from '@sick/shared';

import { ClientBlogPostsRoutingModule } from './client-blog-post-routing.module';
import { PostListComponent } from './feature/post-list/post-list.component';
import { PostItemComponent } from './ui/post-item/post-item.component';

@NgModule({
  imports: [CommonModule, ClientBlogPostsRoutingModule, MaterialModule, HttpClientModule, SharedModule, NgOptimizedImage],
  declarations: [PostListComponent, PostItemComponent],
  exports: [PostListComponent],
})
export class ClientBlogPostsModule {}
