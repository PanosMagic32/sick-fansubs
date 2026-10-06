/**
 * Service-worker registration guard.
 *
 * The registration is production-only and failure-swallowed: the app must
 * work fully without a worker, and Vite dev / bun tests must never install
 * one. These tests pin the guard shape and the hourly update scheduling.
 */

import { describe, expect, test } from "bun:test";

import { registerServiceWorker } from "./service-worker.js";

function stubServiceWorker(
  register: (path: string) => Promise<{ update: () => Promise<void> }>,
) {
  const calls: string[] = [];
  const original = Object.getOwnPropertyDescriptor(navigator, "serviceWorker");
  Object.defineProperty(navigator, "serviceWorker", {
    value: {
      register: (path: string) => {
        calls.push(path);
        return register(path);
      },
    },
    configurable: true,
    writable: true,
  });
  return {
    calls,
    restore: () => {
      if (original) {
        Object.defineProperty(navigator, "serviceWorker", original);
      } else {
        delete (navigator as { serviceWorker?: unknown }).serviceWorker;
      }
    },
  };
}

function stubSetInterval() {
  const captured: { fn: () => void; ms: number }[] = [];
  const original = window.setInterval;
  Object.defineProperty(window, "setInterval", {
    value: (fn: () => void, ms: number) => {
      captured.push({ fn, ms });
      return 1;
    },
    writable: true,
    configurable: true,
  });
  return {
    captured,
    restore: () => {
      Object.defineProperty(window, "setInterval", {
        value: original,
        writable: true,
        configurable: true,
      });
    },
  };
}

describe("registerServiceWorker", () => {
  test("skips registration when not in production", () => {
    const stub = stubServiceWorker(() =>
      Promise.resolve({ update: () => Promise.resolve() }),
    );
    try {
      registerServiceWorker(false, navigator);
      expect(stub.calls).toEqual([]);
    } finally {
      stub.restore();
    }
  });

  test("skips registration when the browser has no service worker", () => {
    // The guard checks 'serviceWorker' in navigator — deleting the
    // property after construction pins that the CURRENT shape wins, and
    // the register spy proves the call never happens.
    const calls: string[] = [];
    const fake = {
      serviceWorker: {
        register: (path: string) => {
          calls.push(path);
          return Promise.resolve({ update: () => Promise.resolve() });
        },
      },
    };
    delete (fake as { serviceWorker?: unknown }).serviceWorker;
    registerServiceWorker(true, fake as unknown as Navigator);
    expect(calls).toEqual([]);
  });

  test("registers /sw.js and schedules the hourly update check", async () => {
    let updates = 0;
    const stub = stubServiceWorker(() =>
      Promise.resolve({
        update: () => {
          updates++;
          return Promise.resolve();
        },
      }),
    );
    const interval = stubSetInterval();
    try {
      registerServiceWorker(true, navigator);
      await Promise.resolve();
      expect(stub.calls).toEqual(["/sw.js"]);
      expect(interval.captured).toHaveLength(1);
      expect(interval.captured[0]?.ms).toBe(60 * 60 * 1000);
      // The scheduled callback must actually run the update check.
      interval.captured[0]?.fn();
      expect(updates).toBe(1);
    } finally {
      interval.restore();
      stub.restore();
    }
  });

  test("honors a custom update interval", async () => {
    const stub = stubServiceWorker(() =>
      Promise.resolve({ update: () => Promise.resolve() }),
    );
    const interval = stubSetInterval();
    try {
      registerServiceWorker(true, navigator, 5_000);
      await Promise.resolve();
      expect(interval.captured[0]?.ms).toBe(5_000);
    } finally {
      interval.restore();
      stub.restore();
    }
  });

  test("swallows registration failure — no error state", async () => {
    const stub = stubServiceWorker(() => Promise.reject(new Error("denied")));
    try {
      registerServiceWorker(true, navigator);
      await Promise.resolve();
      expect(stub.calls).toEqual(["/sw.js"]);
    } finally {
      stub.restore();
    }
  });
});
