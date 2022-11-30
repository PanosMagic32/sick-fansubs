import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterModule } from '@angular/router';

import { MaterialModule } from '@sick/material';

import { SidenavComponent } from './sidenav.component';

@NgModule({
    declarations: [SidenavComponent],
    imports: [CommonModule, MaterialModule, RouterModule],
    exports: [SidenavComponent],
})
export class SidenavModule {}
