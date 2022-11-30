import { NgModule } from '@angular/core';
import { RouterModule, Routes } from '@angular/router';

import { AboutDetailComponent } from './feature/about-detail/about-detail.component';

const routes: Routes = [
    {
        path: '',
        component: AboutDetailComponent,
    },
];

@NgModule({
    imports: [RouterModule.forChild(routes)],
    exports: [RouterModule],
})
export class ClientAboutRoutingModule {}
