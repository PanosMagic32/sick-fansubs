/**
 * submit-errors unit tests: the retry countdown's ticking math (stubbed
 * timers, no real sleeps) and the mapper's connection / 429 branches.
 * The pages' integration (disabled submit, label swap, re-enable) is
 * pinned in the page suites.
 */

import { describe, expect, test } from "bun:test";

import {
  ApiError,
  NetworkError,
  RequestTimeoutError,
} from "@shared/api/client.js";
import { el } from "@shared/catalog/el.js";

import { mapSubmitError, startRetryCountdown } from "./submit-errors.js";

/** Stub Date.now + setInterval/clearInterval; drive ticks manually. */
function stubTimers(initialNow = 1_000_000) {
  const originalSetInterval = globalThis.setInterval;
  const originalClearInterval = globalThis.clearInterval;
  const originalNow = Date.now;

  let fakeNow = initialNow;
  const pending: Array<() => void> = [];
  let cleared = 0;

  globalThis.setInterval = ((fn: () => void) => {
    pending.push(fn);
    return pending.length; // 1-based id, like a real interval
  }) as unknown as typeof setInterval;
  globalThis.clearInterval = ((id: number) => {
    cleared++;
    delete pending[id - 1]; // deregister the tick, like the real thing
  }) as unknown as typeof clearInterval;
  Date.now = () => fakeNow;

  return {
    /** Advance the fake clock and run every still-registered tick. */
    advance(ms: number) {
      fakeNow += ms;
      for (const tick of pending) {
        if (tick) tick(); // cleared entries leave holes (undefined)
      }
    },
    get pendingTicks() {
      return pending.length;
    },
    get clearCalls() {
      return cleared;
    },
    restore() {
      globalThis.setInterval = originalSetInterval;
      globalThis.clearInterval = originalClearInterval;
      Date.now = originalNow;
    },
  };
}

describe("mapSubmitError", () => {
  const opts = {
    serverField: (field: string): field is "username" => field === "username",
  };

  test("a network failure and a request deadline share the generic banner", () => {
    expect(mapSubmitError(new NetworkError("down"), opts)).toEqual({
      fields: {},
      banner: el.ui.error,
    });
    expect(mapSubmitError(new RequestTimeoutError(), opts)).toEqual({
      fields: {},
      banner: el.ui.error,
    });
  });

  test("a 429 with Retry-After starts the cooldown even without a problem body", () => {
    const err = new ApiError(
      {
        type: "/problems/internal-error",
        title: "Unexpected 429 response",
        status: 429,
      },
      30,
    );

    const result = mapSubmitError(err, opts);

    expect(result.retryAfterSeconds).toBe(30);
    expect(result.banner).toContain("30");
  });

  test("a 429 without the header keeps the cooldown off", () => {
    const err = new ApiError({
      type: "/problems/rate-limited",
      title: "Too many requests",
      status: 429,
    });

    expect(mapSubmitError(err, opts).retryAfterSeconds).toBeUndefined();
  });
});

describe("startRetryCountdown", () => {
  test("ticks the full window down to zero and stops", () => {
    const timers = stubTimers();
    try {
      const seen: number[] = [];
      startRetryCountdown(3, (left) => seen.push(left));

      // Immediate first tick: the label starts at the full window.
      expect(seen).toEqual([3]);
      expect(timers.pendingTicks).toBe(1);

      timers.advance(1000);
      expect(seen).toEqual([3, 2]);
      timers.advance(1000);
      expect(seen).toEqual([3, 2, 1]);
      timers.advance(1000);
      expect(seen).toEqual([3, 2, 1, 0]);
      expect(timers.clearCalls).toBe(1);
    } finally {
      timers.restore();
    }
  });

  test("rounds sub-second remainders up (ceiling)", () => {
    const timers = stubTimers();
    try {
      const seen: number[] = [];
      startRetryCountdown(5, (left) => seen.push(left));

      // 1.5s elapsed → 3.5s left → shows 4, not 3.
      timers.advance(1500);
      expect(seen).toEqual([5, 4]);
    } finally {
      timers.restore();
    }
  });

  test("a zero or negative window never starts an interval", () => {
    const timers = stubTimers();
    try {
      const seen: number[] = [];
      const cancel = startRetryCountdown(0, (left) => seen.push(left));
      expect(seen).toEqual([0]);
      expect(timers.pendingTicks).toBe(0);
      cancel(); // must be a harmless no-op
      expect(timers.clearCalls).toBe(0);
    } finally {
      timers.restore();
    }
  });

  test("cancel stops further ticks", () => {
    const timers = stubTimers();
    try {
      const seen: number[] = [];
      const cancel = startRetryCountdown(10, (left) => seen.push(left));
      timers.advance(1000);
      expect(seen).toEqual([10, 9]);

      cancel();
      expect(timers.clearCalls).toBe(1);

      timers.advance(9000);
      // No further ticks after cancel.
      expect(seen).toEqual([10, 9]);
    } finally {
      timers.restore();
    }
  });
});
