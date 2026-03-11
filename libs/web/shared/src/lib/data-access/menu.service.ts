import { BreakpointObserver } from '@angular/cdk/layout';
import { inject, Injectable } from '@angular/core';
import { type Observable, map, shareReplay } from 'rxjs';

@Injectable({ providedIn: 'root' })
export class MenuService {
  private readonly breakpointObserver = inject(BreakpointObserver);

  /** ≤1050px — show hamburger, collapse nav */
  isHandset$: Observable<boolean> = this.breakpointObserver.observe('(max-width: 1050px)').pipe(
    map((result) => result.matches),
    shareReplay(),
  );

  /** ≤1050px — alias kept for template compatibility */
  isSmall$: Observable<boolean> = this.isHandset$;

  /** 1051px–1530px — show icons-only nav */
  isMedium$: Observable<boolean> = this.breakpointObserver.observe('(min-width: 1051px) and (max-width: 1530px)').pipe(
    map((result) => result.matches),
    shareReplay(),
  );
}
