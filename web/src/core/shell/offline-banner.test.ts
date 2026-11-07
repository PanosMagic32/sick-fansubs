import { afterEach, describe, expect, test } from "bun:test";

import { readSheet, readSource, stubProperty } from "@shared/api/test-utils.js";
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
  test("keeps the live region mounted while the browser reports a connection", async () => {
    stubOnline(true);
    const banner = await renderBanner();

    // The host stays in the layout: removing it would remove the live region
    // together with the text it is about to receive.
    expect(banner.hasAttribute("hidden")).toBe(false);
    const status = root(banner).querySelector('[role="status"]');
    expect(status).not.toBeNull();
    expect(status?.textContent?.trim()).toBe("");
    expect(root(banner).querySelector(".banner")).toBeNull();
  });

  test("shows the state sentence on connect when the page is opened offline", async () => {
    stubOnline(false);
    const banner = await renderBanner();

    // The persistent live region carries the announcement; the visible
    // strip is aria-hidden so the sentence is not read twice.
    const status = root(banner).querySelector('[role="status"]');
    expect(status?.textContent?.trim()).toBe(el.offline.message);
    const strip = root(banner).querySelector(".banner");
    expect(strip?.getAttribute("aria-hidden")).toBe("true");
    expect(strip?.textContent?.trim()).toBe(el.offline.message);
    expect(banner.hasAttribute("hidden")).toBe(false);
    // No dismiss control (ruling): the strip reports, it never asks.
    expect(strip?.querySelector("button, [role='button'], a")).toBeNull();
  });

  test("follows the online/offline events", async () => {
    stubOnline(true);
    const banner = await renderBanner();
    expect(root(banner).querySelector(".banner")).toBeNull();
    // The live region exists (empty) before the transition, so the text
    // assignment is announced.
    expect(root(banner).querySelector('[role="status"]')).not.toBeNull();

    stubOnline(false);
    window.dispatchEvent(new Event("offline"));
    await banner.updateComplete;
    expect(root(banner).querySelector(".banner")).not.toBeNull();

    stubOnline(true);
    window.dispatchEvent(new Event("online"));
    await banner.updateComplete;
    expect(root(banner).querySelector(".banner")).toBeNull();
    expect(
      root(banner).querySelector('[role="status"]')?.textContent?.trim(),
    ).toBe("");
  });

  test("reads the property, not the event kind (a stale event never desyncs the strip)", async () => {
    stubOnline(true);
    const banner = await renderBanner();

    // The browser still reports a connection — the event alone must not
    // show the strip.
    window.dispatchEvent(new Event("offline"));
    await banner.updateComplete;

    expect(root(banner).querySelector(".banner")).toBeNull();
    expect(
      root(banner).querySelector('[role="status"]')?.textContent?.trim(),
    ).toBe("");
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
    const source = await readSource(
      new URL("./offline-banner.ts", import.meta.url),
    );
    expect(source).not.toMatch(/serviceWorker|caches|registration/);
  });

  test("pins the slim, main-column-width CSS contract", async () => {
    const styles = await readSheet(
      new URL("./offline-banner.css", import.meta.url),
    );
    // Same width as the main content column (the 80rem wide-route cap).
    const host = styles.match(/:host \{([^}]*)\}/)?.[1] ?? "";
    expect(host).toContain("max-width: 80rem");
    // The host must not carry a vertical margin while online: the live
    // region stays mounted and the visible strip owns the top gap.
    expect(host).not.toContain("margin: 12px");
    const bannerRule = styles.match(/\.banner \{([^}]*)\}/)?.[1] ?? "";
    expect(bannerRule).toContain("margin-top: 12px");
    // Slim: it shares the donation strip's height so the two stack evenly
    // on the home route.
    expect(styles).toMatch(/padding: 0\.45rem 1rem/);
  });
});
