import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';

import { HeaderModule } from './header/header.module';
import { SidenavModule } from './sidenav/sidenav.module';
import { FooterModule } from './footer/footer.module';
import { NoContentComponent } from './no-content/no-content.component';

@NgModule({
    imports: [CommonModule, HeaderModule, SidenavModule, FooterModule],
    exports: [HeaderModule, SidenavModule, FooterModule, NoContentComponent],
    declarations: [NoContentComponent],
})
export class SharedModule {}
