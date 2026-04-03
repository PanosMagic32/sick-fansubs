import { BreakpointObserver } from '@angular/cdk/layout';
import { inject, Injectable } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { map } from 'rxjs';

@Injectable({ providedIn: 'root' })
export class MenuService {
  private readonly breakpointObserver = inject(BreakpointObserver);

  /** ≤1050px — show hamburger, collapse nav */
  readonly isHandset = toSignal(
    this.breakpointObserver.observe('(max-width: 1050px)').pipe(map((result) => result.matches)),
    { initialValue: false },
  );

  /** ≤1050px — alias kept for template compatibility */
  readonly isSmall = this.isHandset;

  /** 1051px–1530px — show icons-only nav */
  readonly isMedium = toSignal(
    this.breakpointObserver.observe('(min-width: 1051px) and (max-width: 1530px)').pipe(map((result) => result.matches)),
    { initialValue: false },
  );
}
