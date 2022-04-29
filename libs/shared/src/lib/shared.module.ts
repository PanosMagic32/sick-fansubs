import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';

import { HeaderModule } from './header/header.module';
import { SidenavModule } from './sidenav/sidenav.module';
import { FooterModule } from './footer/footer.module';

@NgModule({
  imports: [CommonModule, HeaderModule, SidenavModule, FooterModule],
  exports: [HeaderModule, SidenavModule, FooterModule],
})
export class SharedModule {}
