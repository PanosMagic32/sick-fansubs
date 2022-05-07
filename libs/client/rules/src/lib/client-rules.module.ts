import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';

import { MaterialModule } from '@sick/material';

import { ClientRulesRoutingModule } from './client-rules-routing.module';
import { RulesDetailComponent } from './feature/rules-detail/rules-detail.component';

@NgModule({
  imports: [CommonModule, ClientRulesRoutingModule, MaterialModule],
  declarations: [RulesDetailComponent],
  exports: [],
})
export class ClientRulesModule {}
