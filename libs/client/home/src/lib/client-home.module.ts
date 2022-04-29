import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';

import { MaterialModule } from '@sick/material';

import { ClientHomeRoutingModule } from './client-home-routing.module';
import { PostListComponent } from './feature/post-list/post-list.component';
import { PostDetailComponent } from './feature/post-detail/post-detail.component';
import { PostItemComponent } from './ui/post-item/post-item.component';

@NgModule({
  imports: [CommonModule, ClientHomeRoutingModule, MaterialModule],
  declarations: [PostListComponent, PostDetailComponent, PostItemComponent],
  exports: [PostListComponent, PostDetailComponent],
})
export class ClientHomeModule {}
