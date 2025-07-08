import { BreakpointObserver, Breakpoints } from '@angular/cdk/layout';
import { inject, Injectable } from '@angular/core';
import { type Observable, map, shareReplay } from 'rxjs';

@Injectable({ providedIn: 'root' })
export class MenuService {
  private readonly breakpointObserver = inject(BreakpointObserver);

  isHandset$: Observable<boolean> = this.breakpointObserver.observe(Breakpoints.Handset).pipe(
    map((result) => result.matches),
    shareReplay(),
  );

  isSmall$: Observable<boolean> = this.breakpointObserver.observe(Breakpoints.Small).pipe(
    map((result) => result.matches),
    shareReplay(),
  );

  isMedium$: Observable<boolean> = this.breakpointObserver.observe(Breakpoints.Medium).pipe(
    map((result) => result.matches),
    shareReplay(),
  );
}
