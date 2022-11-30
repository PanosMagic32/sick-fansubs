import { Breakpoints, BreakpointObserver } from '@angular/cdk/layout';
import { Injectable } from '@angular/core';
import { Observable, map, shareReplay } from 'rxjs';

@Injectable({
    providedIn: 'root',
})
export class MenuService {
    isHandset$: Observable<boolean> = this.breakpointObserver.observe(Breakpoints.Handset).pipe(
        map((result) => result.matches),
        shareReplay()
    );

    isSmall$: Observable<boolean> = this.breakpointObserver.observe(Breakpoints.Small).pipe(
        map((result) => result.matches),
        shareReplay()
    );

    isMedium$: Observable<boolean> = this.breakpointObserver.observe(Breakpoints.Medium).pipe(
        map((result) => result.matches),
        shareReplay()
    );

    constructor(private breakpointObserver: BreakpointObserver) {}
}
