import { describe, expect, test, beforeEach, afterEach } from "bun:test";

import {
  stripCssComments,
  stubAbortSignalTimeout,
} from "@shared/api/test-utils.js";
import { el } from "@shared/catalog/el.js";
import { BUY_ME_A_COFFEE_URL } from "@shared/config/links.js";

import "./app-footer.js";
import type { AppFooter } from "./app-footer.js";

const footerStyles = stripCssComments(
  await Bun.file(new URL("./app-footer.css", import.meta.url)).text(),
);

function renderFooter(): AppFooter {
  const el = document.createElement("app-footer");
  document.body.appendChild(el);
  return el;
}

describe("app-footer", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  test("renders role contentinfo", async () => {
    globalThis.fetch = (() => new Promise(() => {})) as unknown as typeof fetch;
    const el = renderFooter();
    await el.updateComplete;
    expect(el.getAttribute("role")).toBe("contentinfo");
    el.remove();
  });

  test("shows loading state initially", async () => {
    globalThis.fetch = (() => new Promise(() => {})) as unknown as typeof fetch;
    const footer = renderFooter();
    await footer.updateComplete;

    const status = footer.shadowRoot?.querySelector("span");
    expect(status?.textContent).toContain("Φόρτωση");
    // The health status resolves after the first paint — the live region
    // announces the loading → online/offline flip to screen readers.
    expect(status?.getAttribute("role")).toBe("status");
    // Loading is Greek — the lang="en" scope exists only for the
    // deliberate Online/Offline anglicisms.
    expect(status?.querySelector('span[lang="en"]')).toBeNull();

    footer.remove();
  });

  test("shows online when /health/ready returns ok", async () => {
    let requested = "";
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      requested = typeof input === "string" ? input : String(input);
      return new Response("{}", { status: 200 });
    }) as unknown as typeof fetch;
    const footer = renderFooter();

    // Initial render (loading) — capture the live-region span NOW: the
    // announcement contract is that Lit keeps the SAME element across
    // the update (replacing it would reset the screen-reader region).
    await footer.updateComplete;
    const statusSpan = footer.shadowRoot?.querySelector('span[role="status"]');

    // Let the async _checkHealth resolve and trigger re-render.
    await new Promise((r) => setTimeout(r, 10));
    await footer.updateComplete;

    // Reference equality pins that the span was not replaced.
    expect(footer.shadowRoot?.querySelector('span[role="status"]')).toBe(
      statusSpan,
    );
    expect(statusSpan?.querySelector(".status-dot.online")).not.toBeNull();

    // Deliberate anglicism: the English "Online" renders inside lang="en"
    // so assistive tech pronounces it correctly.
    expect(statusSpan?.querySelector('span[lang="en"]')?.textContent).toBe(
      el.ui.statusOnline,
    );
    // The badge reports READINESS — the endpoint itself is the contract.
    expect(requested).toBe("/health/ready");

    footer.remove();
  });

  test("shows offline when /health/ready fails", async () => {
    let requested = "";
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      requested = typeof input === "string" ? input : String(input);
      throw new Error("down");
    }) as unknown as typeof fetch;
    const footer = renderFooter();

    await footer.updateComplete;
    await new Promise((r) => setTimeout(r, 10));
    await footer.updateComplete;

    const dot = footer.shadowRoot?.querySelector(".status-dot.offline");
    expect(dot).not.toBeNull();

    const statusSpan = footer.shadowRoot?.querySelector('span[role="status"]');
    expect(statusSpan?.querySelector('span[lang="en"]')?.textContent).toBe(
      el.ui.statusOffline,
    );
    expect(requested).toBe("/health/ready");

    footer.remove();
  });

  test("the probe carries its own deadline; a timeout reads offline", async () => {
    const timeout = stubAbortSignalTimeout();
    try {
      globalThis.fetch = (async () =>
        Promise.reject(
          new DOMException("timed out", "TimeoutError"),
        )) as unknown as typeof fetch;
      const footer = renderFooter();
      await footer.updateComplete;
      await new Promise((r) => setTimeout(r, 10));
      await footer.updateComplete;

      // A timed-out probe is not an API call: it surfaces as offline, and
      // the deadline is this probe's own (not the shared client timeout).
      expect(timeout.spy.mock.calls[0]?.[0]).toBe(5_000);
      expect(
        footer.shadowRoot?.querySelector(".status-dot.offline"),
      ).not.toBeNull();
      footer.remove();
    } finally {
      timeout.restore();
    }
  });

  test("reads version from meta tag", async () => {
    const meta = document.createElement("meta");
    meta.name = "app-version";
    meta.content = "1.2.3";
    document.head.appendChild(meta);

    globalThis.fetch = (() =>
      Promise.resolve(
        new Response("{}", { status: 200 }),
      )) as unknown as typeof fetch;
    const el = renderFooter();

    await el.updateComplete;
    await new Promise((r) => setTimeout(r, 10));
    await el.updateComplete;

    const versionSpan = [
      ...(el.shadowRoot?.querySelectorAll("span") ?? []),
    ].find((s) => s.textContent?.startsWith("v"));
    expect(versionSpan?.textContent).toBe("v1.2.3");

    meta.remove();
    el.remove();
  });

  test("renders the brand line with the year", async () => {
    globalThis.fetch = (() =>
      Promise.resolve(
        new Response("{}", { status: 200 }),
      )) as unknown as typeof fetch;
    const el = renderFooter();

    await el.updateComplete;
    await new Promise((r) => setTimeout(r, 10));
    await el.updateComplete;

    const small = el.shadowRoot?.querySelector("small");
    expect(small?.textContent).toContain("Sick-HQ");

    el.remove();
  });

  test("three zones: status left, support center, copyright + version right", async () => {
    globalThis.fetch = (() =>
      Promise.resolve(
        new Response("{}", { status: 200 }),
      )) as unknown as typeof fetch;
    const footer = renderFooter();

    await footer.updateComplete;
    await new Promise((r) => setTimeout(r, 10));
    await footer.updateComplete;

    // Zone order: left status, center support, right copyright — the
    // host's direct children.
    const zones = [...(footer.shadowRoot?.children ?? [])];
    expect(zones.map((z) => z.className)).toEqual([
      "footer-left",
      "footer-center",
      "footer-right",
    ]);

    // The support line: catalog text + external donation link.
    const support = footer.shadowRoot?.querySelector(".footer-center");
    expect(support?.textContent?.replace(/\s+/g, " ").trim()).toContain(
      el.donation.supportText,
    );
    const link = support?.querySelector("a");
    expect(link?.getAttribute("href")).toBe(BUY_ME_A_COFFEE_URL);
    expect(link?.getAttribute("target")).toBe("_blank");
    expect(link?.getAttribute("rel")).toBe("noopener noreferrer");
    expect(link?.querySelector('span[lang="en"]')?.textContent?.trim()).toBe(
      el.about.donateLabel,
    );

    // Copyright + version sit in the right zone.
    const right = footer.shadowRoot?.querySelector(".footer-right");
    expect(right?.querySelector("small")?.textContent).toContain("Sick-HQ");

    footer.remove();
  });

  test("mobile footer stacks only its inner zones (CSS contract)", () => {
    // happy-dom cannot evaluate media queries — pin the source: at the
    // mobile tier (700px) the INNER sections change direction (support
    // column with its divider, stacked copyright/version) while the host
    // itself stays a row with space-between. The band is extracted whole
    // first, then each rule body — a match can never reach a later rule.
    const mobile =
      footerStyles.match(
        /@media \(max-width: 700px\) \{([\s\S]*?)\n\}/s,
      )?.[1] ?? "";
    const center = mobile.match(/\.footer-center \{([^}]*)\}/)?.[1] ?? "";
    expect(center).toContain("flex-direction: column");
    const divider = mobile.match(/\.support-divider \{([^}]*)\}/)?.[1] ?? "";
    expect(divider).toContain("display: block");
    const right = mobile.match(/\.footer-right \{([^}]*)\}/)?.[1] ?? "";
    expect(right).toContain("flex-direction: column");
    // The divider is hidden in the desktop inline row.
    expect(footerStyles).toMatch(/\.support-divider \{[^}]*display: none/);
  });
});
