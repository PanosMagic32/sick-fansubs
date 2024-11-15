import { ChangeDetectionStrategy, Component, inject, type OnInit } from '@angular/core';
import { RouterOutlet } from '@angular/router';

import { MatSidenav, MatSidenavContainer, MatSidenavContent } from '@angular/material/sidenav';

import { HeaderComponent, SidenavComponent, TokenService } from '@web/shared';

@Component({
  selector: 'sf-root',
  styleUrl: './app.component.scss',
  template: `
    <mat-sidenav-container>
      <mat-sidenav #sidenav role="navigation" fixedInViewport="true">
        <sf-sidenav (sidenavClose)="sidenav.close()" />
      </mat-sidenav>

      <mat-sidenav-content>
        <sf-header (sidenavToggle)="sidenav.toggle()" />

        <div class="shell">
          <section class="section left"></section>

          <section class="section main">
            <router-outlet></router-outlet>
          </section>

          <section class="section right"></section>
        </div>
      </mat-sidenav-content>
    </mat-sidenav-container>
  `,
  standalone: true,
  imports: [RouterOutlet, MatSidenav, MatSidenavContainer, MatSidenavContent, HeaderComponent, SidenavComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AppComponent implements OnInit {
  private readonly tokenService = inject(TokenService);

  ngOnInit() {
    this.tokenService.getUserIDFromToken();
  }
}
