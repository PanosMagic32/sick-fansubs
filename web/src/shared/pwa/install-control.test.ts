import { afterEach, beforeEach, describe, expect, test } from "bun:test";

import { stubProperty, waitFor } from "@shared/api/test-utils.js";
import { el } from "@shared/catalog/el.js";

import { resetInstallPrompt, watchInstallPrompt } from "./install-prompt.js";
import "./install-control.js";
import type { InstallControl } from "./install-control.js";

const UA_IPHONE =
  "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15";

const restores: Array<() => void> = [];

/** Shadow-stub with an immediate restore registration (afterEach). */
function stub(target: object, name: string, value: unknown): void {
  restores.push(stubProperty(target, name, value));
}

function stubNotificationPermission(permission: string): void {
  // stubProperty, not assignment: happy-dom ships no `Notification`, so an
  // assigned restore would leave an own `Notification: undefined` shadow on
  // the shared window (push-settings.test.ts does the same).
  stub(globalThis, "Notification", { permission });
}

/** Fires a fake `beforeinstallprompt`; returns its prompt-call counter. */
function fireInstallPrompt(): { promptCalls: () => number } {
  let calls = 0;
  const event = new Event("beforeinstallprompt", { cancelable: true });
  Object.assign(event, {
    prompt: () => {
      calls += 1;
      return Promise.resolve();
    },
  });
  window.dispatchEvent(event);
  return { promptCalls: () => calls };
}

async function renderControl(): Promise<InstallControl> {
  const control = document.createElement("install-control");
  document.body.appendChild(control);
  await control.updateComplete;
  return control;
}

function root(control: InstallControl): HTMLElement {
  return control.shadowRoot! as unknown as HTMLElement;
}

beforeEach(() => {
  resetInstallPrompt();
  watchInstallPrompt(window);
});

afterEach(() => {
  document.body.innerHTML = "";
  resetInstallPrompt();
  while (restores.length > 0) restores.pop()!();
});

describe("install-control", () => {
  test("renders nothing (and stays out of the layout) without a prompt outside iOS", async () => {
    const control = await renderControl();

    expect(root(control).querySelector(".install")).toBeNull();
    expect(root(control).querySelector(".install-hint")).toBeNull();
    expect(control.hasAttribute("hidden")).toBe(true);
  });

  test("renders the install button once the browser offers a prompt, and replays it once", async () => {
    const { promptCalls } = fireInstallPrompt();
    const control = await renderControl();

    const button = root(control).querySelector(
      ".install",
    ) as HTMLButtonElement | null;
    expect(button?.textContent?.trim()).toBe(el.install.button);
    expect(button?.type).toBe("button");
    expect(control.hasAttribute("hidden")).toBe(false);

    button!.click();
    await control.updateComplete;

    expect(promptCalls()).toBe(1);
    // The stashed event is single-use: the control goes away until the
    // browser decides to fire a fresh one.
    expect(root(control).querySelector(".install")).toBeNull();
    expect(control.hasAttribute("hidden")).toBe(true);
  });

  test("hides the control after appinstalled", async () => {
    fireInstallPrompt();
    const control = await renderControl();
    expect(root(control).querySelector(".install")).not.toBeNull();

    window.dispatchEvent(new Event("appinstalled"));
    await control.updateComplete;

    expect(root(control).querySelector(".install")).toBeNull();
  });

  test("never renders while the app already runs installed (iOS flag)", async () => {
    stub(navigator, "standalone", true);
    fireInstallPrompt();
    const control = await renderControl();

    expect(root(control).querySelector(".install")).toBeNull();
    expect(root(control).querySelector(".install-hint")).toBeNull();
    expect(control.hasAttribute("hidden")).toBe(true);
  });

  test("never renders while the app already runs installed (display-mode)", async () => {
    stub(window, "matchMedia", (query: string) => ({
      matches: query === "(display-mode: standalone)",
      media: query,
    }));
    fireInstallPrompt();
    const control = await renderControl();

    expect(root(control).querySelector(".install")).toBeNull();
  });

  test("hides the control when this browser already allowed notifications", async () => {
    stubNotificationPermission("granted");
    fireInstallPrompt();
    const control = await renderControl();

    expect(root(control).querySelector(".install")).toBeNull();
  });

  test("re-evaluates when the notification permission changes", async () => {
    const listeners = new Set<() => void>();
    stub(navigator, "permissions", {
      query: () =>
        Promise.resolve({
          addEventListener: (_type: string, fn: () => void) => {
            listeners.add(fn);
          },
          removeEventListener: (_type: string, fn: () => void) => {
            listeners.delete(fn);
          },
        }),
    });
    fireInstallPrompt();
    await renderControl();
    // Let the (async) permission watch attach before the change fires.
    await waitFor(() => listeners.size > 0);
    const control = document.querySelector("install-control")!;
    expect(root(control).querySelector(".install")).not.toBeNull();

    // The sibling push-settings section just granted the permission.
    stubNotificationPermission("granted");
    for (const listener of listeners) listener();
    await control.updateComplete;

    expect(root(control).querySelector(".install")).toBeNull();
    expect(control.hasAttribute("hidden")).toBe(true);

    // The watch is torn down with the element.
    control.remove();
    expect(listeners.size).toBe(0);
  });

  test("iOS in browser mode states the manual share-menu path", async () => {
    stub(navigator, "userAgent", UA_IPHONE);
    const control = await renderControl();

    const hint = root(control).querySelector(".install-hint");
    expect(hint?.textContent?.trim()).toBe(el.install.iosHint);
    // The manual path is an instruction, never a dead button.
    expect(root(control).querySelector(".install")).toBeNull();
    expect(control.hasAttribute("hidden")).toBe(false);
  });

  test("iOS in standalone mode shows neither branch", async () => {
    stub(navigator, "userAgent", UA_IPHONE);
    stub(navigator, "standalone", true);
    const control = await renderControl();

    expect(root(control).querySelector(".install")).toBeNull();
    expect(root(control).querySelector(".install-hint")).toBeNull();
  });

  test("the iOS hint disappears once notifications are already allowed", async () => {
    stub(navigator, "userAgent", UA_IPHONE);
    stubNotificationPermission("granted");
    const control = await renderControl();

    expect(root(control).querySelector(".install-hint")).toBeNull();
  });

  test("stops listening once disconnected", async () => {
    const control = await renderControl();
    control.remove();

    fireInstallPrompt();

    const internals = control as unknown as { _available: boolean };
    expect(internals._available).toBe(false);
  });
});
