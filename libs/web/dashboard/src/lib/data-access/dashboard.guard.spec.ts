import { TestBed } from '@angular/core/testing';
import { RedirectCommand, Router } from '@angular/router';
import { describe, expect, it, vi } from 'vitest';

import { TokenService } from '@web/shared';

import { dashboardGuard } from './dashboard.guard';

describe('dashboardGuard', () => {
  it('allows access for staff users', () => {
    const tokenServiceMock = {
      canAccessDashboard: vi.fn(() => true),
    } as unknown as TokenService;

    const routerMock = {
      parseUrl: vi.fn((url: string) => ({ toString: () => url })),
    } as unknown as Router;

    TestBed.configureTestingModule({
      providers: [
        { provide: TokenService, useValue: tokenServiceMock },
        { provide: Router, useValue: routerMock },
      ],
    });

    const result = TestBed.runInInjectionContext(() => dashboardGuard({} as never, {} as never));

    expect(result).toBe(true);
  });

  it('redirects non-staff users to login', () => {
    const tokenServiceMock = {
      canAccessDashboard: vi.fn(() => false),
    } as unknown as TokenService;

    const urlTree = { toString: () => '/auth/login' };
    const routerMock = {
      parseUrl: vi.fn(() => urlTree),
    } as unknown as Router;

    TestBed.configureTestingModule({
      providers: [
        { provide: TokenService, useValue: tokenServiceMock },
        { provide: Router, useValue: routerMock },
      ],
    });

    const result = TestBed.runInInjectionContext(() => dashboardGuard({} as never, {} as never));

    expect(result).toBeInstanceOf(RedirectCommand);
    expect(routerMock.parseUrl).toHaveBeenCalledWith('/auth/login');
  });
});
