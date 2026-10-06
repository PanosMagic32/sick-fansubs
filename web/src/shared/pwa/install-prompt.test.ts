import { afterEach, beforeAll, describe, expect, test } from "bun:test";

import { stubProperty } from "@shared/api/test-utils.js";

import {
  consumeInstallPrompt,
  installPromptAvailable,
  notificationsGranted,
  resetInstallPrompt,
  subscribeInstallPrompt,
  watchInstallPrompt,
} from "./install-prompt.js";

/** Fires a fake `beforeinstallprompt`; returns its prompt-call counter. */
function fireInstallPrompt(): { promptCalls: () => number; event: Event } {
  let calls = 0;
  const event = new Event("beforeinstallprompt", { cancelable: true });
  Object.assign(event, {
    prompt: () => {
      calls += 1;
      return Promise.resolve();
    },
  });
  window.dispatchEvent(event);
  return { promptCalls: () => calls, event };
}

beforeAll(() => {
  watchInstallPrompt(window);
});

afterEach(() => {
  resetInstallPrompt();
});

describe("install prompt store", () => {
  test("captures the event, suppresses the browser prompt, and notifies", () => {
    let notified = 0;
    const unsubscribe = subscribeInstallPrompt(() => {
      notified += 1;
    });
    try {
      const { event } = fireInstallPrompt();

      expect(event.defaultPrevented).toBe(true);
      expect(installPromptAvailable()).toBe(true);
      expect(notified).toBe(1);
    } finally {
      unsubscribe();
    }
  });

  test("unsubscribing stops the notifications", () => {
    let notified = 0;
    subscribeInstallPrompt(() => {
      notified += 1;
    })();

    fireInstallPrompt();
    expect(notified).toBe(0);
  });

  test("wiring twice is a no-op (the capture is page-level)", () => {
    // core/index.ts wires once; a second wiring must not double-notify for
    // one event (both hosts would still read the same stash).
    watchInstallPrompt(window);

    let notified = 0;
    const unsubscribe = subscribeInstallPrompt(() => {
      notified += 1;
    });
    try {
      fireInstallPrompt();
      expect(notified).toBe(1);
    } finally {
      unsubscribe();
    }
  });

  test("replays the prompt once and clears the stash", async () => {
    const { promptCalls } = fireInstallPrompt();

    await consumeInstallPrompt();
    expect(promptCalls()).toBe(1);
    expect(installPromptAvailable()).toBe(false);

    // Single-use: the stashed event is gone, so a second call is a no-op.
    await consumeInstallPrompt();
    expect(promptCalls()).toBe(1);
  });

  test("consuming with no stashed event resolves without prompting", async () => {
    await expect(consumeInstallPrompt()).resolves.toBeUndefined();
  });

  test("appinstalled clears the stash and notifies", () => {
    fireInstallPrompt();
    expect(installPromptAvailable()).toBe(true);

    let notified = 0;
    const unsubscribe = subscribeInstallPrompt(() => {
      notified += 1;
    });
    try {
      window.dispatchEvent(new Event("appinstalled"));
      expect(installPromptAvailable()).toBe(false);
      expect(notified).toBe(1);
    } finally {
      unsubscribe();
    }
  });
});

describe("notificationsGranted", () => {
  test("is false without a Notification API", () => {
    // stubProperty shadows the global and restores by DELETE when happy-dom
    // exposed none (it ships no Notification) — an assigned `undefined`
    // would leave an own property that still answers `"Notification" in
    // globalThis` for every later file in this worker.
    const restore = stubProperty(globalThis, "Notification", undefined);
    try {
      expect(notificationsGranted()).toBe(false);
    } finally {
      restore();
    }
  });

  test("mirrors the permission value", () => {
    const restore = stubProperty(globalThis, "Notification", {
      permission: "granted",
    });
    try {
      expect(notificationsGranted()).toBe(true);
      (
        globalThis as unknown as { Notification: { permission: string } }
      ).Notification.permission = "default";
      expect(notificationsGranted()).toBe(false);
    } finally {
      restore();
    }
  });
});
