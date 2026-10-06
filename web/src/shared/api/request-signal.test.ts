import { afterEach, describe, expect, test } from "bun:test";

import { stubProperty } from "./test-utils.js";
import { API_REQUEST_TIMEOUT_MS, requestSignal } from "./request-signal.js";

let restoreTimeoutStub: (() => void) | undefined;

afterEach(() => {
  restoreTimeoutStub?.();
  restoreTimeoutStub = undefined;
});

describe("requestSignal", () => {
  test("composes the page controller with the client timeout", () => {
    const controller = new AbortController();
    const signal = requestSignal(controller);

    expect(signal).toBeInstanceOf(AbortSignal);
    expect(signal.aborted).toBe(false);

    // The page's teardown abort propagates to the composed signal.
    controller.abort();
    expect(signal.aborted).toBe(true);
  });

  test("the shared timeout bound stays at 15s (under the server's 30s)", () => {
    // Pinned so an accidental bump cannot strand a page on the spinner for
    // longer than the server's own write deadline.
    expect(API_REQUEST_TIMEOUT_MS).toBe(15_000);
  });

  test("the shared deadline participates in the composed signal", () => {
    const deadline = AbortSignal.abort(
      new DOMException("The operation was timed out", "TimeoutError"),
    );
    restoreTimeoutStub = stubProperty(AbortSignal, "timeout", () => deadline);

    const signal = requestSignal(new AbortController());
    expect(signal.aborted).toBe(true);
    expect(signal.reason?.name).toBe("TimeoutError");
  });
});
