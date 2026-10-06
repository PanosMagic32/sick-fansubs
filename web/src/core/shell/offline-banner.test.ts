import { afterEach, describe, expect, test } from "bun:test";

import {
  stripCssComments,
  stripSourceComments,
  stubProperty,
} from "@shared/api/test-utils.js";
import { el } from "@shared/catalog/el.js";

import "./offline-banner.js";
import type { OfflineBanner } from "./offline-banner.js";

const restores: Array<() => void> = [];

/** Shadow-stub navigator.onLine (restored in afterEach — one window per worker). */
function stubOnline(online: boolean): void {
  restores.push(stubProperty(navigator, "onLine", online));
}

async function renderBanner(): Promise<OfflineBanner> {
  const banner = document.createElement("offline-banner") as OfflineBanner;
  document.body.appendChild(banner);
  await banner.updateComplete;
  return banner;
}

function root(banner: OfflineBanner): HTMLElement {
  return banner.shadowRoot! as unknown as HTMLElement;
}

afterEach(() => {
  document.body.innerHTML = "";
  while (restores.length > 0) restores.pop()!();
});

describe("offline-banner", () => {
  test("renders nothing (and takes no space) while the browser reports a connection", async () => {
    stubOnline(true);
    const banner = await renderBanner();

    expect(banner.hasAttribute("hidden")).toBe(true);
    expect(root(banner).querySelector(".banner")).toBeNull();
  });

  test("shows the state sentence on connect when the page is opened offline", async () => {
    stubOnline(false);
    const banner = await renderBanner();

    const strip = root(banner).querySelector(".banner");
    expect(strip?.getAttribute("role")).toBe("status");
    expect(strip?.textContent?.trim()).toBe(el.offline.message);
    expect(banner.hasAttribute("hidden")).toBe(false);
    // No dismiss control (ruling): the strip reports, it never asks.
    expect(strip?.querySelector("button, [role='button'], a")).toBeNull();
  });

  test("follows the online/offline events", async () => {
    stubOnline(true);
    const banner = await renderBanner();
    expect(root(banner).querySelector(".banner")).toBeNull();

    stubOnline(false);
    window.dispatchEvent(new Event("offline"));
    await banner.updateComplete;
    expect(root(banner).querySelector(".banner")).not.toBeNull();

    stubOnline(true);
    window.dispatchEvent(new Event("online"));
    await banner.updateComplete;
    expect(root(banner).querySelector(".banner")).toBeNull();
    expect(banner.hasAttribute("hidden")).toBe(true);
  });

  test("reads the property, not the event kind (a stale event never desyncs the strip)", async () => {
    stubOnline(true);
    const banner = await renderBanner();

    // The browser still reports a connection — the event alone must not
    // show the strip.
    window.dispatchEvent(new Event("offline"));
    await banner.updateComplete;

    expect(root(banner).querySelector(".banner")).toBeNull();
    expect(banner.hasAttribute("hidden")).toBe(true);
  });

  test("stops listening once disconnected", async () => {
    stubOnline(true);
    const banner = await renderBanner();
    banner.remove();

    stubOnline(false);
    window.dispatchEvent(new Event("offline"));

    const internals = banner as unknown as { _offline: boolean };
    expect(internals._offline).toBe(false);
  });

  test("consults no service-worker state", async () => {
    // Source-level pin (the index.test.ts pattern): the worker is
    // production-only and its page cache is partial, so the strip must
    // never read worker/cache state to decide what to say. Comments are
    // stripped first so prose can never decide this pin.
    const source = stripSourceComments(
      await Bun.file(new URL("./offline-banner.ts", import.meta.url)).text(),
    );
    expect(source).not.toMatch(/serviceWorker|caches|registration/);
  });

  test("pins the slim, main-column-width CSS contract", async () => {
    const styles = stripCssComments(
      await Bun.file(new URL("./offline-banner.css", import.meta.url)).text(),
    );
    // Same width as the main content column (the 80rem wide-route cap).
    const host = styles.match(/:host \{([^}]*)\}/)?.[1] ?? "";
    expect(host).toContain("max-width: 80rem");
    // The author :host display beats the UA [hidden] rule, so the guard is
    // what actually keeps the strip out of the layout while online.
    const hidden = styles.match(/:host\(\[hidden\]\) \{([^}]*)\}/)?.[1] ?? "";
    expect(hidden).toContain("display: none");
    // Slim: it shares the donation strip's height so the two stack evenly
    // on the home route.
    expect(styles).toMatch(/padding: 0\.45rem 1rem/);
  });
});
