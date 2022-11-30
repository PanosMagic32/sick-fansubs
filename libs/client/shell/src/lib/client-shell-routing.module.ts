import { NgModule } from '@angular/core';
import { RouterModule, Routes } from '@angular/router';

const routes: Routes = [
    {
        path: '',
        loadChildren: () => import('@sick/client/home').then((m) => m.ClientHomeModule),
    },
    {
        path: 'projects',
        loadChildren: () => import('@sick/client/projects').then((m) => m.ClientProjectsModule),
    },
    // {
    //   path: 'rules',
    //   loadChildren: () => import('@sick/client/rules').then((m) => m.ClientRulesModule),
    // },
    {
        path: 'about',
        loadChildren: () => import('@sick/client/about').then((m) => m.ClientAboutModule),
    },
    // {
    //   path: 'auth',
    //   loadChildren: () => import('@sick/client/auth').then((m) => m.ClientAuthModule),
    // },
];

@NgModule({
    imports: [RouterModule.forChild(routes)],
    exports: [RouterModule],
})
export class ClientShellRoutingModule {}
