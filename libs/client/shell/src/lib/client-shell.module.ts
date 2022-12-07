import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';

import { ClientShellRoutingModule } from './client-shell-routing.module';
import { ShellComponent } from './shell/shell.component';

@NgModule({
  imports: [CommonModule, ClientShellRoutingModule],
  declarations: [ShellComponent],
  exports: [ShellComponent],
})
export class ClientShellModule {}
