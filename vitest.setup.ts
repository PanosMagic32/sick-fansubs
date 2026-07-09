import 'reflect-metadata';
import '@angular/compiler';

import { NgModule } from '@angular/core';
import { getTestBed } from '@angular/core/testing';
import { BrowserTestingModule, platformBrowserTesting } from '@angular/platform-browser/testing';
import { Logger } from '@nestjs/common';

// Silence NestJS logs during tests to keep output clean.
Logger.overrideLogger(false as unknown as string[]);

// Angular TestBed initialization for jsdom-based tests.
// Matches Angular 21's built-in @angular/build:unit-test setup.
// Guard with window check: node-environment tests skip this.
if (typeof window !== 'undefined') {
  const ANGULAR_TESTBED_SETUP = Symbol.for('@angular/cli/testbed-setup');
  if (!(globalThis as Record<symbol, boolean>)[ANGULAR_TESTBED_SETUP]) {
    (globalThis as Record<symbol, boolean>)[ANGULAR_TESTBED_SETUP] = true;

    @NgModule({ providers: [] })
    class TestModule {}

    getTestBed().initTestEnvironment([BrowserTestingModule, TestModule], platformBrowserTesting(), {
      errorOnUnknownElements: true,
      errorOnUnknownProperties: true,
    });
  }
}
