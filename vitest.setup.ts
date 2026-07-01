import '@angular/compiler';
import { getTestBed } from '@angular/core/testing';

// Initialize Angular TestBed for jsdom-based service tests.
if (typeof window !== 'undefined') {
  try {
    getTestBed().initTestEnvironment(BrowserDynamicTestingModule, platformBrowserDynamicTesting());
  } catch {
    // Ignore if already initialized by another setup path.
  }
}
