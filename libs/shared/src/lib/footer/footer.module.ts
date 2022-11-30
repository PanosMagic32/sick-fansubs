import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';

import { MaterialModule } from '@sick/material';

import { FooterComponent } from './footer.component';

@NgModule({
    declarations: [FooterComponent],
    imports: [CommonModule, MaterialModule],
    exports: [FooterComponent],
})
export class FooterModule {}
