import { NgModule } from '@angular/core';
import { RouterModule, Routes } from '@angular/router';

import { RulesDetailComponent } from './feature/rules-detail/rules-detail.component';

const routes: Routes = [
  {
    path: '',
    component: RulesDetailComponent,
  },
];

@NgModule({
  imports: [RouterModule.forChild(routes)],
  exports: [RouterModule],
})
export class ClientRulesRoutingModule {}
