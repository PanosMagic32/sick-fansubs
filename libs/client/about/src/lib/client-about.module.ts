import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';

import { MaterialModule } from '@sick/material';

import { ClientAboutRoutingModule } from './client-about-routing.module';
import { AboutDetailComponent } from './feature/about-detail/about-detail.component';

@NgModule({
  imports: [CommonModule, ClientAboutRoutingModule, MaterialModule],
  declarations: [AboutDetailComponent],
  exports: [AboutDetailComponent],
})
export class ClientAboutModule {}
