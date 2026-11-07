import { describe, expect, test, beforeEach, afterEach } from "bun:test";

import {
  readSheet,
  stubAbortSignalTimeout,
  stubProperty,
} from "@shared/api/test-utils.js";
import { copyrightNotice, el } from "@shared/catalog/el.js";
import { BUY_ME_A_COFFEE_URL, COMMUNITY_LINKS } from "@shared/config/links.js";

import "./app-footer.js";
import type { AppFooter } from "./app-footer.js";

const footerStyles = await readSheet(
  new URL("./app-footer.css", import.meta.url),
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
    // The shared happy-dom body is reused across files: a failed test that
    // leaves a footer attached must not leak into the next one.
    document.body.innerHTML = "";
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

  test("re-probes on reconnect and when the tab becomes visible", async () => {
    let calls = 0;
    globalThis.fetch = (async () => {
      calls += 1;
      if (calls === 2) return new Response("{}", { status: 200 });
      throw new Error("down");
    }) as unknown as typeof fetch;
    const visibility = stubProperty(document, "visibilityState", "visible");
    try {
      const footer = renderFooter();
      await footer.updateComplete;
      await new Promise((r) => setTimeout(r, 10));
      await footer.updateComplete;
      expect(
        footer.shadowRoot?.querySelector(".status-dot.offline"),
      ).not.toBeNull();

      // A reconnect must not leave the footer contradicting the offline
      // strip that just disappeared.
      window.dispatchEvent(new Event("online"));
      await new Promise((r) => setTimeout(r, 10));
      await footer.updateComplete;
      expect(
        footer.shadowRoot?.querySelector(".status-dot.online"),
      ).not.toBeNull();
      expect(calls).toBe(2);

      document.dispatchEvent(new Event("visibilitychange"));
      await new Promise((r) => setTimeout(r, 10));
      await footer.updateComplete;
      expect(calls).toBe(3);
      footer.remove();
    } finally {
      visibility();
    }
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
    expect(small?.textContent).toBe(copyrightNotice(new Date().getFullYear()));

    el.remove();
  });

  test("renders the community links from the shared order", async () => {
    globalThis.fetch = (() =>
      Promise.resolve(
        new Response("{}", { status: 200 }),
      )) as unknown as typeof fetch;
    const footer = renderFooter();

    await footer.updateComplete;
    await new Promise((r) => setTimeout(r, 10));
    await footer.updateComplete;

    const social = footer.shadowRoot?.querySelector(".footer-social");
    expect(social?.tagName).toBe("NAV");
    expect(social?.getAttribute("aria-label")).toBe(el.ui.communityLinks);

    const links = [...(social?.querySelectorAll("a") ?? [])];
    expect(links.map((a) => a.getAttribute("href"))).toEqual(
      COMMUNITY_LINKS.map((link) => link.url),
    );
    expect(links.map((a) => a.getAttribute("target"))).toEqual(
      COMMUNITY_LINKS.map(() => "_blank"),
    );
    expect(links.map((a) => a.getAttribute("rel"))).toEqual(
      COMMUNITY_LINKS.map(() => "noopener noreferrer"),
    );
    expect(links.map((a) => a.className)).toEqual(
      COMMUNITY_LINKS.map((link) => `social-${link.tone}`),
    );
    // The brand labels are deliberate anglicisms: each accessible name is
    // the sr-only label inside lang="en".
    expect(
      links.map((a) => a.querySelector('span[lang="en"]')?.textContent?.trim()),
    ).toEqual(COMMUNITY_LINKS.map((link) => link.label));
    // The new-tab context is Greek and stays outside the lang="en" scope.
    expect(
      links.map((a) =>
        a.querySelector(".sr-only:not([lang])")?.textContent?.trim(),
      ),
    ).toEqual(COMMUNITY_LINKS.map(() => el.nav.opensInNewTab));

    // Every glyph is distinct, hidden from assistive tech, and the hidden
    // label is the name source (an aria-label or title would override it).
    const paths = links.map((a) => a.querySelector("path")?.getAttribute("d"));
    expect(new Set(paths).size).toBe(COMMUNITY_LINKS.length);
    expect(paths.every((d) => typeof d === "string" && d.length > 0)).toBe(
      true,
    );
    for (const link of links) {
      const svg = link.querySelector("svg");
      expect(svg?.getAttribute("aria-hidden")).toBe("true");
      expect(svg?.querySelector("title")).toBeNull();
      const label = link.querySelector('.sr-only[lang="en"]');
      expect(label?.hasAttribute("aria-hidden")).toBe(false);
      expect(link.hasAttribute("aria-label")).toBe(false);
      expect(link.hasAttribute("title")).toBe(false);
    }

    footer.remove();
  });

  test("the center zone orders the social row above the support line", async () => {
    globalThis.fetch = (() =>
      Promise.resolve(
        new Response("{}", { status: 200 }),
      )) as unknown as typeof fetch;
    const footer = renderFooter();

    await footer.updateComplete;
    await new Promise((r) => setTimeout(r, 10));
    await footer.updateComplete;

    const center = footer.shadowRoot?.querySelector(".footer-center");
    expect(
      [...(center?.children ?? [])].map((child) => child.className),
    ).toEqual(["footer-social", "footer-support"]);

    footer.remove();
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
    const support = footer.shadowRoot?.querySelector(".footer-support");
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
    // The external anchor announces its new tab to assistive tech.
    expect(link?.querySelector(".sr-only")?.textContent?.trim()).toBe(
      el.nav.opensInNewTab,
    );

    // Copyright + version sit in the right zone.
    const right = footer.shadowRoot?.querySelector(".footer-right");
    expect(right?.querySelector("small")?.textContent).toContain(
      el.ui.copyrightHolder,
    );

    footer.remove();
  });

  test("a probe that resolves after teardown never paints a stale status", async () => {
    const resolvers: Array<(res: Response) => void> = [];
    globalThis.fetch = (() =>
      new Promise<Response>((resolve) => {
        resolvers.push(resolve);
      })) as unknown as typeof fetch;

    const footer = renderFooter();
    await footer.updateComplete;

    // Teardown happens while the first probe is in flight.
    footer.remove();
    resolvers[0]!(new Response("{}", { status: 503 }));
    await Promise.resolve();

    // Reconnect starts a new probe; the old failure must not answer for it.
    document.body.appendChild(footer);
    await footer.updateComplete;
    expect(
      footer.shadowRoot?.querySelector(".status-dot.loading"),
    ).not.toBeNull();

    resolvers[1]!(new Response("{}", { status: 200 }));
    await new Promise((r) => setTimeout(r, 10));
    await footer.updateComplete;
    expect(
      footer.shadowRoot?.querySelector(".status-dot.online"),
    ).not.toBeNull();

    footer.remove();
  });

  test("footer places social links above support and stacks support on mobile (CSS contract)", () => {
    // happy-dom cannot evaluate media queries — pin the source. The center
    // zone is a column at every width (social row first, support line
    // second); only the SUPPORT line stacks at ≤700px, while the host stays
    // a row with space-between.
    const center = footerStyles.match(/\.footer-center \{([^}]*)\}/)?.[1] ?? "";
    expect(center).toContain("flex-direction: column");

    const social = footerStyles.match(/\.footer-social \{([^}]*)\}/)?.[1] ?? "";
    expect(social).toContain("display: flex");

    // Each social link keeps a 2.5rem round target around a 1.5rem glyph.
    const socialLink =
      footerStyles.match(/\.footer-social a \{([^}]*)\}/)?.[1] ?? "";
    expect(socialLink).toContain("width: 2.5rem");
    expect(socialLink).toContain("height: 2.5rem");
    expect(socialLink).toContain("border-radius: 50%");
    expect(footerStyles).toMatch(/\.footer-social svg \{[^}]*width: 1\.5rem/);

    const mobile =
      footerStyles.match(
        /@media \(max-width: 700px\) \{([\s\S]*?)\n\}/s,
      )?.[1] ?? "";
    const support = mobile.match(/\.footer-support \{([^}]*)\}/)?.[1] ?? "";
    expect(support).toContain("flex-direction: column");
    const divider = mobile.match(/\.support-divider \{([^}]*)\}/)?.[1] ?? "";
    expect(divider).toContain("display: block");
    const right = mobile.match(/\.footer-right \{([^}]*)\}/)?.[1] ?? "";
    expect(right).toContain("flex-direction: column");
    // The divider is hidden in the desktop inline support row.
    expect(footerStyles).toMatch(/\.support-divider \{[^}]*display: none/);
  });
});
