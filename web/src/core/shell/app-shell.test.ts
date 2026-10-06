import { describe, expect, test, beforeEach, afterEach, mock } from "bun:test";

import "./app-shell.js";
import type { AppShell } from "./app-shell.js";
import "@features/blog/blog-detail-page.js";
import type { BlogDetailPage } from "@features/blog/blog-detail-page.js";
import "@features/projects/project-detail-page.js";
import type { ProjectDetailPage } from "@features/projects/project-detail-page.js";
import { navigate, NAVIGATE_EVENT, routes } from "@core/router.js";
import { el } from "@shared/catalog/el.js";
import {
  resetHappyDOMURL,
  setHappyDOMURL,
  stripCssComments,
} from "@shared/api/test-utils.js";
import { LitElement } from "lit";

// The stylesheet is read from disk rather than through the `?inline`
// import (contract pins must not ride the bundler's CSS loader mapping),
// with comments stripped so prose can never decide a pin.
const shellStyles = stripCssComments(
  await Bun.file(new URL("./app-shell.css", import.meta.url)).text(),
);

// ── Helpers ─────────────────────────────────────────────────────────

function renderShell(): AppShell {
  const el = document.createElement("app-shell") as AppShell;
  document.body.appendChild(el);
  return el;
}

/** Read one detail page's hero title through the shared content card (the
   page composes the card, which renders the title in its own shadow root). */
async function contentCardTitle(page: LitElement | null): Promise<void> {
  const card = page?.shadowRoot?.querySelector("content-card") ?? null;
  await (card as LitElement | null)?.updateComplete;
  expect(card?.shadowRoot?.querySelector(".title")?.textContent?.trim()).toBe(
    "Τίτλος δοκιμής",
  );
}

function teardown() {
  document.body.innerHTML = "";
}

let originalPushState: unknown;
let originalReplaceState: unknown;
let originalFetch: typeof globalThis.fetch;

beforeEach(() => {
  // Spy on history methods to observe SPA navigations (restored afterEach).
  originalFetch = globalThis.fetch;
  originalPushState = window.history.pushState;
  originalReplaceState = window.history.replaceState;
  (window.history as unknown as { pushState: unknown }).pushState = mock(
    () => {},
  );
  (window.history as unknown as { replaceState: unknown }).replaceState = mock(
    () => {},
  );
  // The shell renders app-footer, which probes /health/ready on mount —
  // keep its fetch deterministic instead of hitting the network.
  globalThis.fetch = (async () =>
    new Response("{}", { status: 200 })) as unknown as typeof fetch;
});

afterEach(() => {
  teardown();
  resetHappyDOMURL();
  (window.history as unknown as { pushState: unknown }).pushState =
    originalPushState;
  (window.history as unknown as { replaceState: unknown }).replaceState =
    originalReplaceState;
  document.title = "";
  globalThis.fetch = originalFetch;
});

// ── Forced-change routing ───────────────────────────────────────────

describe("app-shell forced-change routing", () => {
  test("a flagged session routes to /account", async () => {
    const el = document.createElement("app-shell") as AppShell;
    (el as unknown as { session: unknown }).session = {
      state: {
        status: "authenticated",
        mustChangePassword: true,
        user: { id: "1", username: "alice", role: "user" },
      },
    };
    document.body.appendChild(el);
    await el.updateComplete;

    expect(window.history.replaceState).toHaveBeenCalledWith(
      {},
      "",
      "/account",
    );
    el.remove();
  });

  test("an unflagged session does not force-route", async () => {
    const el = document.createElement("app-shell") as AppShell;
    (el as unknown as { session: unknown }).session = {
      state: {
        status: "authenticated",
        mustChangePassword: false,
        user: { id: "1", username: "alice", role: "user" },
      },
    };
    document.body.appendChild(el);
    await el.updateComplete;

    for (const call of (window.history.replaceState as ReturnType<typeof mock>)
      .mock.calls) {
      expect(call[2]).not.toBe("/account");
    }
    el.remove();
  });
});

// ── Shell rendering ─────────────────────────────────────────────────

describe("app-shell", () => {
  test("renders skip-to-content link from catalog", async () => {
    const el = renderShell();
    await el.updateComplete;

    const link = el.shadowRoot?.querySelector(".skip-link");
    expect(link?.textContent).toBe("Παράλειψη πλοήγησης");
    expect(link?.getAttribute("href")).toBe("#main-content");

    el.remove();
  });

  test("renders main with id for skip-link target", async () => {
    const el = renderShell();
    await el.updateComplete;

    const main = el.shadowRoot?.querySelector("main");
    expect(main?.getAttribute("id")).toBe("main-content");

    el.remove();
  });

  test("background dots are aria-hidden", async () => {
    const el = renderShell();
    await el.updateComplete;

    const bg = el.shadowRoot?.querySelector(".bg");
    expect(bg?.getAttribute("aria-hidden")).toBe("true");

    el.remove();
  });

  test("defaults to dark theme", async () => {
    const el = renderShell();
    await el.updateComplete;

    expect(el.classList.contains("light")).toBe(false);

    el.remove();
  });

  test("renders header and footer", async () => {
    const el = renderShell();
    await el.updateComplete;

    const header = el.shadowRoot?.querySelector("app-header");
    const footer = el.shadowRoot?.querySelector("app-footer");
    expect(header).not.toBeNull();
    expect(footer).not.toBeNull();

    el.remove();
  });

  test("full-route roots fill the screen at FHD and cap only at ultrawide", () => {
    // happy-dom cannot measure layout, so the width contract is pinned
    // against the CSS source: .full-route has NO cap at FHD and below
    // (edge-to-edge), and at >1920px it caps at 1872px — PIXELS, not rem
    // (the 112.5% root font would silently mis-size a rem cap) — the
    // exact FHD content width (1920px − 2×24px main padding), so the
    // transition at the boundary is seamless and gaps appear only beyond
    // it.
    const rule = shellStyles.match(/main > \.full-route \{([^}]*)\}/s)?.[1];
    expect(rule).toContain("max-width: none");

    const ultra = shellStyles.match(
      /@media \(min-width: 1920\.01px\) \{([\s\S]*?)\n\}/s,
    )?.[1];
    expect(ultra).toContain("max-width: 1872px");
  });

  test("wide-route roots take the 80rem content cap", () => {
    const rule =
      shellStyles.match(/main > \.wide-route \{([^}]*)\}/s)?.[1] ?? "";
    expect(rule).toContain("max-width: 80rem");
  });

  test("the accent hover fill is darker, so a white label stays readable", async () => {
    // The owner's ruling: a filled button's label is white, so its HOVER
    // fill must not be a lighter blue. `--sf-accent-light` is LIGHT in the
    // dark theme (#8aaae0), which left a hovering label at ~2.4:1.
    // happy-dom resolves no tokens: pinned against the sources.
    const dark = shellStyles.match(/:host \{([^}]*)\}/s)?.[1] ?? "";
    const light = shellStyles.match(/:host\(\.light\) \{([^}]*)\}/s)?.[1] ?? "";
    // The exact pair, so a lighter replacement cannot satisfy the pin.
    expect(dark).toContain("--sf-accent-strong: #2a55ab;");
    expect(light).toContain("--sf-accent-strong: #1f4685;");

    // The shared primary hover is the darker fill, in one place —
    // a consumer that drops the class loses the hover, which the control
    // vocabulary's own test cannot see; the account-page suite pins the
    // page-side token rule.
    const controlStyles = stripCssComments(
      await Bun.file(
        new URL("../../shared/ui/controls.css", import.meta.url),
      ).text(),
    );
    const primaryHover =
      controlStyles.match(
        /\.button--primary:hover:not\(:disabled\) \{([^}]*)\}/,
      )?.[1] ?? "";
    expect(primaryHover).toContain("var(--sf-accent-strong");
    expect(primaryHover).not.toContain("var(--sf-accent-light");
  });

  test("the about page's tinted-row link pair keeps its contrast values", () => {
    // happy-dom resolves no tokens and composites no color-mix: the exact
    // values are pinned against the source. The light value clears 4.5:1
    // on all five brand tints; the dark keeps --sf-accent-light, which
    // clears them in the dark theme.
    const dark = shellStyles.match(/:host \{([^}]*)\}/s)?.[1] ?? "";
    const light = shellStyles.match(/:host\(\.light\) \{([^}]*)\}/s)?.[1] ?? "";
    expect(dark).toContain("--sf-link-on-tint: #8aaae0;");
    expect(light).toContain("--sf-link-on-tint: #2855a3;");

    // The five brand tones the tinted rows mix over — exact values in both
    // themes, so a replacement cannot pass on the names alone.
    const brandPairs: Array<[string, string, string]> = [
      ["--sf-brand-facebook", "#1877f2", "#1467d8"],
      ["--sf-brand-discord", "#5865f2", "#4a56d8"],
      ["--sf-brand-github", "#8b949e", "#57606a"],
      ["--sf-brand-tracker", "#2bb3a3", "#178f83"],
      ["--sf-brand-coffee", "#e6a817", "#a97b0a"],
    ];
    for (const [name, darkValue, lightValue] of brandPairs) {
      expect(dark).toContain(`${name}: ${darkValue};`);
      expect(light).toContain(`${name}: ${lightValue};`);
    }
  });

  test("the mono stack is the one token both themes declare", () => {
    // Typography has one home: the --sf-font-mono token in both theme
    // blocks; a shadow root never repeats the family literal.
    const dark = shellStyles.match(/:host \{([^}]*)\}/s)?.[1] ?? "";
    const light = shellStyles.match(/:host\(\.light\) \{([^}]*)\}/s)?.[1] ?? "";
    expect(dark).toContain('--sf-font-mono: "IBM Plex Mono", monospace;');
    expect(light).toContain('--sf-font-mono: "IBM Plex Mono", monospace;');
  });

  test("routes are top-aligned and only the single-card auth routes center", () => {
    // happy-dom cannot measure layout, so the rule is pinned against the
    // CSS source. The vertical centering is what made every route's chrome
    // move when its content changed height.
    const mainRule = shellStyles.match(/(?:^|\n)main \{([^}]*)\}/s)?.[1] ?? "";
    expect(mainRule).toContain("align-items: flex-start");
    expect(mainRule).not.toContain("align-items: center");

    // The opt-in the auth routes and the 404 fallback carry — without this
    // rule their .center-route class would be inert.
    const optIn =
      shellStyles.match(/main > \.center-route \{([^}]*)\}/s)?.[1] ?? "";
    expect(optIn).toContain("align-self: center");
  });

  test("a route root can always shrink to the column", () => {
    // The root is a flex item, so its automatic minimum size is its content's
    // min-content width — one unshrinkable row (the admin form's download
    // fields, the admin tab row) would widen the whole page past the viewport
    // and give the site a horizontal scrollbar. min-width: 0 is the guard; the
    // rows themselves own the fix for what overflows.
    const rule = shellStyles.match(/(?:^|\n)main > \* \{([^}]*)\}/s)?.[1] ?? "";
    expect(rule).toContain("min-width: 0");
    // The width contract the guard must not disturb.
    expect(rule).toContain("width: 100%");
    expect(rule).toContain("max-width: 48rem");
  });
});

// ── Click interception (SPA navigation) ─────────────────────────────

function dispatchClick(
  anchor: HTMLElement,
  init: MouseEventInit = {},
): MouseEvent {
  const event = new MouseEvent("click", {
    bubbles: true,
    composed: true,
    cancelable: true,
    ...init,
  });
  anchor.dispatchEvent(event);
  return event;
}

/**
 * Prove the shell's interceptor is live before a negative case: a
 * same-origin anchor must be prevented and routed. The pushState mock is
 * cleared afterwards so the case's own not-called assertion starts clean —
 * without this, a negative test passes with the whole listener removed.
 */
async function expectInterceptionLive(): Promise<void> {
  const anchor = document.createElement("a");
  anchor.setAttribute("href", "/interception-probe");
  document.body.appendChild(anchor);

  const event = dispatchClick(anchor);

  expect(event.defaultPrevented).toBe(true);
  expect(window.history.pushState).toHaveBeenCalled();
  (window.history.pushState as ReturnType<typeof mock>).mockClear();
  anchor.remove();
}

describe("_onLinkClick", () => {
  beforeEach(() => {
    // A deterministic origin: the interception contract compares link and
    // page origins, and the shared window's URL leaks across worker files —
    // never inherit it. The same-origin/hash tests use setAttribute so the
    // raw href keeps its relative/hash form (assigning .href serializes to
    // an absolute URL under a real base, and the handler branches on the
    // RAW attribute).
    setHappyDOMURL("https://sickfansubs.com/");
  });

  test("intercepts same-origin link and calls pushState", async () => {
    const shell = renderShell();
    await shell.updateComplete;

    const anchor = document.createElement("a");
    anchor.setAttribute("href", "/sign-in");
    document.body.appendChild(anchor);

    const event = dispatchClick(anchor);

    expect(event.defaultPrevented).toBe(true);
    expect(window.history.pushState).toHaveBeenCalled();

    anchor.remove();
    shell.remove();
  });

  test("intercepts an anchor inside a component's shadow root (the content card)", async () => {
    const shell = renderShell();
    await shell.updateComplete;

    // The card's own shadow root holds the anchor; the event's composed
    // path is what the handler reads, so a card click routes like any
    // other link.
    const card = document.createElement("content-card");
    (card as { shape?: string }).shape = "row";
    (card as { href?: string }).href = "/blog/p1";
    document.body.appendChild(card);
    await (card as { updateComplete: Promise<unknown> }).updateComplete;

    const anchor = card.shadowRoot?.querySelector("a.frame");
    expect(anchor).not.toBeNull();
    const event = dispatchClick(anchor as HTMLElement);

    expect(event.defaultPrevented).toBe(true);
    expect(window.history.pushState).toHaveBeenCalledWith(
      expect.anything(),
      "",
      "/blog/p1",
    );

    card.remove();
    shell.remove();
  });

  test("does not intercept cross-origin links", async () => {
    const shell = renderShell();
    await shell.updateComplete;
    await expectInterceptionLive();

    const anchor = document.createElement("a");
    anchor.href = "https://example.com/other";
    document.body.appendChild(anchor);

    const event = dispatchClick(anchor);

    expect(event.defaultPrevented).toBe(false);
    expect(window.history.pushState).not.toHaveBeenCalled();

    anchor.remove();
    shell.remove();
  });

  test("does not intercept modified clicks (ctrl)", async () => {
    const shell = renderShell();
    await shell.updateComplete;

    const anchor = document.createElement("a");
    anchor.href = "/sign-in";
    document.body.appendChild(anchor);

    const event = dispatchClick(anchor, { ctrlKey: true });

    expect(event.defaultPrevented).toBe(false);

    anchor.remove();
    shell.remove();
  });

  test("does not intercept mailto: links", async () => {
    const shell = renderShell();
    await shell.updateComplete;
    await expectInterceptionLive();

    const anchor = document.createElement("a");
    anchor.href = "mailto:test@example.com";
    document.body.appendChild(anchor);

    const event = dispatchClick(anchor);

    expect(event.defaultPrevented).toBe(false);
  });

  test("does not intercept magnet: links (torrent handler, not SPA)", async () => {
    // Detail-page download links use magnet: hrefs. Any non-http(s) scheme
    // must reach the browser/platform natively — a magnet URI would
    // otherwise be misrouted through pushState as a fake relative path.
    const shell = renderShell();
    await shell.updateComplete;
    await expectInterceptionLive();

    const anchor = document.createElement("a");
    anchor.setAttribute("href", "magnet:?xt=urn:btih:aaa");
    document.body.appendChild(anchor);

    const event = dispatchClick(anchor);

    expect(event.defaultPrevented).toBe(false);
    expect(window.history.pushState).not.toHaveBeenCalled();
  });

  test("does not intercept scheme links with mixed-case schemes", async () => {
    // Schemes are case-insensitive (RFC 3986 §3.1): `JavaScript:` and
    // `MAILTO:` must be skipped exactly like their lowercase forms.
    // setAttribute keeps the raw attribute intact — the .href property
    // would normalize the scheme to lowercase.
    const shell = renderShell();
    await shell.updateComplete;
    await expectInterceptionLive();

    for (const href of ["JavaScript:alert(1)", "MAILTO:test@example.com"]) {
      const anchor = document.createElement("a");
      anchor.setAttribute("href", href);
      document.body.appendChild(anchor);

      const event = dispatchClick(anchor);

      expect(event.defaultPrevented, href).toBe(false);
      expect(window.history.pushState, href).not.toHaveBeenCalled();

      anchor.remove();
    }

    shell.remove();
  });

  test("does not intercept hash-only links (skip-link)", async () => {
    const shell = renderShell();
    await shell.updateComplete;
    await expectInterceptionLive();

    const anchor = document.createElement("a");
    anchor.setAttribute("href", "#main-content");
    document.body.appendChild(anchor);

    const event = dispatchClick(anchor);

    // Hash-only navigation stays native for fragment scrolling/focus.
    expect(event.defaultPrevented).toBe(false);
    expect(window.history.pushState).not.toHaveBeenCalled();

    anchor.remove();
    shell.remove();
  });

  test("does not intercept clicks on non-anchor elements", async () => {
    const shell = renderShell();
    await shell.updateComplete;

    const button = document.createElement("button");
    document.body.appendChild(button);

    const event = dispatchClick(button);

    expect(event.defaultPrevented).toBe(false);

    button.remove();
    shell.remove();
  });

  test("syncs currentPath on click navigation", async () => {
    const shell = renderShell();
    await shell.updateComplete;

    const anchor = document.createElement("a");
    anchor.setAttribute("href", "/sign-in");
    document.body.appendChild(anchor);

    dispatchClick(anchor);

    await shell.updateComplete;
    expect((shell as unknown as { currentPath: string }).currentPath).toBe(
      "/sign-in",
    );

    anchor.remove();
    shell.remove();
  });

  test("intercepts a protocol-relative same-origin link", async () => {
    const shell = renderShell();
    await shell.updateComplete;

    const anchor = document.createElement("a");
    anchor.setAttribute("href", "//sickfansubs.com/blog/x");
    document.body.appendChild(anchor);

    const event = dispatchClick(anchor);

    expect(event.defaultPrevented).toBe(true);
    expect(window.history.pushState).toHaveBeenCalledWith(
      expect.anything(),
      "",
      "/blog/x",
    );

    anchor.remove();
    shell.remove();
  });

  test("does not intercept a protocol-relative cross-origin link", async () => {
    const shell = renderShell();
    await shell.updateComplete;
    await expectInterceptionLive();

    const anchor = document.createElement("a");
    anchor.setAttribute("href", "//example.com/x");
    document.body.appendChild(anchor);

    const event = dispatchClick(anchor);

    expect(event.defaultPrevented).toBe(false);
    expect(window.history.pushState).not.toHaveBeenCalled();

    anchor.remove();
    shell.remove();
  });

  test("intercepts a colon inside the query string (no bogus scheme)", async () => {
    const shell = renderShell();
    await shell.updateComplete;

    const anchor = document.createElement("a");
    anchor.setAttribute("href", "/search?q=13:00");
    document.body.appendChild(anchor);

    const event = dispatchClick(anchor);

    expect(event.defaultPrevented).toBe(true);
    expect(window.history.pushState).toHaveBeenCalledWith(
      expect.anything(),
      "",
      "/search?q=13:00",
    );

    anchor.remove();
    shell.remove();
  });

  test("does not intercept middle-click or other buttons", async () => {
    const shell = renderShell();
    await shell.updateComplete;
    await expectInterceptionLive();

    const anchor = document.createElement("a");
    anchor.setAttribute("href", "/sign-in");
    document.body.appendChild(anchor);

    const event = dispatchClick(anchor, { button: 1 });

    expect(event.defaultPrevented).toBe(false);
    expect(window.history.pushState).not.toHaveBeenCalled();

    anchor.remove();
    shell.remove();
  });

  test("does not intercept target or download links", async () => {
    const shell = renderShell();
    await shell.updateComplete;
    await expectInterceptionLive();

    for (const [attr, value] of [
      ["target", "_blank"],
      ["download", ""],
    ] as const) {
      const anchor = document.createElement("a");
      anchor.setAttribute("href", "/sign-in");
      anchor.setAttribute(attr, value);
      document.body.appendChild(anchor);

      const event = dispatchClick(anchor);

      expect(event.defaultPrevented, attr).toBe(false);
      expect(window.history.pushState, attr).not.toHaveBeenCalled();

      anchor.remove();
    }
    shell.remove();
  });

  test("does not intercept when another handler prevented the default", async () => {
    const shell = renderShell();
    await shell.updateComplete;

    const anchor = document.createElement("a");
    anchor.setAttribute("href", "/sign-in");
    anchor.addEventListener("click", (e) => e.preventDefault());
    document.body.appendChild(anchor);

    const event = dispatchClick(anchor);

    expect(event.defaultPrevented).toBe(true);
    expect(window.history.pushState).not.toHaveBeenCalled();

    anchor.remove();
    shell.remove();
  });

  test("does not intercept rel=external links", async () => {
    const shell = renderShell();
    await shell.updateComplete;
    await expectInterceptionLive();

    const anchor = document.createElement("a");
    anchor.setAttribute("href", "/sign-in");
    anchor.setAttribute("rel", "external");
    document.body.appendChild(anchor);

    const event = dispatchClick(anchor);

    expect(event.defaultPrevented).toBe(false);
    expect(window.history.pushState).not.toHaveBeenCalled();

    anchor.remove();
    shell.remove();
  });

  test("does not intercept a query-only link on the current path", async () => {
    // "?q=1" resolves to the CURRENT pathname; a same-path link stays
    // native (a reload), by the interception contract.
    const shell = renderShell();
    await shell.updateComplete;
    await expectInterceptionLive();

    const anchor = document.createElement("a");
    anchor.setAttribute("href", "?q=1");
    document.body.appendChild(anchor);

    const event = dispatchClick(anchor);

    expect(event.defaultPrevented).toBe(false);
    expect(window.history.pushState).not.toHaveBeenCalled();

    anchor.remove();
    shell.remove();
  });
});

// ── Back/forward navigation (popstate) ─────────────────────────────
// The custom navigation layer exists because @lit-labs/router's Router
// never registers listeners under Lit — so popstate handling is
// hand-rolled here and must be pinned (test back/forward behavior).

describe("_onPopState", () => {
  /** Stub window.location.pathname; returns a restore function. */
  function stubLocation(pathname: string): () => void {
    const originalLocation = window.location;
    Object.defineProperty(window, "location", {
      value: { pathname, href: "" },
      writable: true,
      configurable: true,
    });
    return () => {
      Object.defineProperty(window, "location", {
        value: originalLocation,
        writable: true,
        configurable: true,
      });
    };
  }

  test("re-renders the route, syncs currentPath, title, and focus", async () => {
    const shell = renderShell();
    await shell.updateComplete;

    const restore = stubLocation("/register");
    try {
      window.dispatchEvent(new PopStateEvent("popstate"));
      await shell.updateComplete;

      expect((shell as unknown as { currentPath: string }).currentPath).toBe(
        "/register",
      );
      expect(document.title).toContain(el.titles.register);
      expect(shell.shadowRoot?.querySelector("register-page")).not.toBeNull();
      expect(shell.shadowRoot?.activeElement?.id).toBe("main-content");
    } finally {
      restore();
    }

    shell.remove();
  });

  test("Back after a click returns to the previous route", async () => {
    const shell = renderShell();
    await shell.updateComplete;

    // A real document URL is required: interception resolves hrefs against
    // the document base, and this file's window URL is otherwise blank.
    setHappyDOMURL("https://sickfansubs.com/");

    // Forward: SPA click navigation to /sign-in (pushState is mocked).
    const anchor = document.createElement("a");
    anchor.setAttribute("href", "/sign-in");
    document.body.appendChild(anchor);
    dispatchClick(anchor);
    await shell.updateComplete;
    expect((shell as unknown as { currentPath: string }).currentPath).toBe(
      "/sign-in",
    );

    // Back: the browser fires popstate with the previous pathname.
    const restore = stubLocation("/");
    try {
      window.dispatchEvent(new PopStateEvent("popstate"));
      await shell.updateComplete;
      expect((shell as unknown as { currentPath: string }).currentPath).toBe(
        "/",
      );
    } finally {
      restore();
    }

    anchor.remove();
    shell.remove();
  });
});

// ── Programmatic navigation ─────────────────────────────────────────

describe("navigate()", () => {
  test("dispatches NAVIGATE_EVENT with the path", () => {
    const nav = { path: null as string | null };
    const listener = ((e: Event) => {
      nav.path = (e as CustomEvent).detail.path;
    }) as EventListener;
    window.addEventListener(NAVIGATE_EVENT, listener);

    try {
      navigate("/register");
    } finally {
      window.removeEventListener(NAVIGATE_EVENT, listener);
    }

    expect(nav.path).toBe("/register");
  });
});

describe("SPA page-change announcement", () => {
  test("renders the blog detail page for /blog/:id (route → element → fetch chain)", async () => {
    // A routed page whose element core/index.ts never registers renders
    // as an HTMLUnknownElement (silently blank).
    // This pins the whole chain the real app uses on direct navigation:
    // shell goto → route match → <blog-detail-page .postId> → fetch →
    // success render.
    const originalLocation = window.location;
    Object.defineProperty(window, "location", {
      value: { pathname: "/blog/p1", href: "" },
      writable: true,
      configurable: true,
    });
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : input.toString();
      if (url.includes("/blog-posts/")) {
        return new Response(
          JSON.stringify({
            id: "p1",
            title: "Τίτλος δοκιμής",
            subtitle: "",
            description: "Περιγραφή",
            thumbnailUrl: "https://example.com/t.jpg",
            publishedAt: "2026-08-15T18:30:00.000Z",
            updatedAt: "2026-08-15T18:30:00.000Z",
            creator: null,
            updater: null,
            downloads: [],
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }
      return new Response("ok", { status: 200 });
    }) as unknown as typeof fetch;

    try {
      const shell = renderShell();
      await shell.updateComplete;
      // Let the page's fetch + success render settle.
      await new Promise((r) => setTimeout(r, 0));
      await shell.updateComplete;

      const page = shell.shadowRoot?.querySelector(
        "blog-detail-page",
      ) as BlogDetailPage | null;
      expect(page).not.toBeNull();
      expect(page?.postId).toBe("p1");
      // The title lives in the shared content card's shadow root — the
      // detail page composes the hero, it does not render it.
      await contentCardTitle(page);
    } finally {
      Object.defineProperty(window, "location", {
        value: originalLocation,
        writable: true,
        configurable: true,
      });
    }
  });

  test("uses the base blog-detail title for /blog/* paths", async () => {
    // Initial load title mapping: /blog/{id} is a dynamic path, so the
    // shell maps it by prefix to the base post title; the detail page
    // refines document.title with the post title after the fetch.
    const originalLocation = window.location;
    Object.defineProperty(window, "location", {
      value: { pathname: "/blog/p1", href: "" },
      writable: true,
      configurable: true,
    });

    // Keep the detail page from refining the title: a masked 404 lands it
    // in the notFound state, which never touches document.title. This also
    // makes the test independent of whether blog-detail-page is registered
    // (core/index.test.ts registers it in the shared test process).
    globalThis.fetch = (async () =>
      new Response(
        JSON.stringify({
          type: "/problems/not-found",
          title: "Not found",
          status: 404,
        }),
        {
          status: 404,
          headers: { "Content-Type": "application/problem+json" },
        },
      )) as unknown as typeof fetch;

    try {
      const shell = renderShell();
      await shell.updateComplete;

      expect(document.title).toContain(el.titles.blogDetail);
    } finally {
      Object.defineProperty(window, "location", {
        value: originalLocation,
        writable: true,
        configurable: true,
      });
    }
  });

  test("renders the project detail page for /projects/:id (route → element → fetch chain)", async () => {
    // The projects analogue of the blog chain test: a routed page whose
    // element core/index.ts never registers renders as an
    // HTMLUnknownElement (silently blank). This pins the whole chain the
    // real app uses on direct navigation: shell goto → route match →
    // <project-detail-page .projectId> → fetch → success render.
    const originalLocation = window.location;
    Object.defineProperty(window, "location", {
      value: { pathname: "/projects/pr1", href: "" },
      writable: true,
      configurable: true,
    });
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : input.toString();
      if (url.includes("/projects/")) {
        return new Response(
          JSON.stringify({
            id: "pr1",
            title: "Τίτλος δοκιμής",
            description: "Περιγραφή",
            slug: "titlos-dokimis",
            thumbnailUrl: "https://example.com/t.jpg",
            publishedAt: "2026-08-15T18:30:00.000Z",
            updatedAt: "2026-08-15T18:30:00.000Z",
            creator: null,
            updater: null,
            downloads: [],
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }
      return new Response("ok", { status: 200 });
    }) as unknown as typeof fetch;

    try {
      const shell = renderShell();
      await shell.updateComplete;
      // Let the page's fetch + success render settle.
      await new Promise((r) => setTimeout(r, 0));
      await shell.updateComplete;

      const page = shell.shadowRoot?.querySelector(
        "project-detail-page",
      ) as ProjectDetailPage | null;
      expect(page).not.toBeNull();
      expect(page?.projectId).toBe("pr1");
      await contentCardTitle(page);
    } finally {
      Object.defineProperty(window, "location", {
        value: originalLocation,
        writable: true,
        configurable: true,
      });
    }
  });

  test("uses the base projects-detail title for /projects/* paths", async () => {
    // Same contract as the blog-detail base-title test: /projects/{id} is
    // a dynamic path, so the shell maps it by prefix to the base project
    // title; the detail page refines document.title after the fetch. The
    // masked 404 lands the page in notFound, which never touches
    // document.title.
    const originalLocation = window.location;
    Object.defineProperty(window, "location", {
      value: { pathname: "/projects/pr1", href: "" },
      writable: true,
      configurable: true,
    });

    globalThis.fetch = (async () =>
      new Response(
        JSON.stringify({
          type: "/problems/not-found",
          title: "Not found",
          status: 404,
        }),
        {
          status: 404,
          headers: { "Content-Type": "application/problem+json" },
        },
      )) as unknown as typeof fetch;

    try {
      const shell = renderShell();
      await shell.updateComplete;

      expect(document.title).toContain(el.titles.projectsDetail);
    } finally {
      Object.defineProperty(window, "location", {
        value: originalLocation,
        writable: true,
        configurable: true,
      });
    }
  });

  test("uses the catalog about title for /about", async () => {
    const originalLocation = window.location;
    Object.defineProperty(window, "location", {
      value: { pathname: "/about", href: "" },
      writable: true,
      configurable: true,
    });

    try {
      const shell = renderShell();
      await shell.updateComplete;

      expect(document.title).toContain(el.titles.about);
    } finally {
      Object.defineProperty(window, "location", {
        value: originalLocation,
        writable: true,
        configurable: true,
      });
    }
  });

  test("every route in the table gets a real page title", async () => {
    for (const route of routes) {
      const path = (route as { path?: string }).path?.replace(":id", "x");
      if (!path) continue;
      const originalLocation = window.location;
      Object.defineProperty(window, "location", {
        value: { pathname: path, href: "" },
        writable: true,
        configurable: true,
      });
      try {
        const shell = renderShell();
        await shell.updateComplete;
        expect(document.title, path).not.toContain(el.titles.notFound);
        shell.remove();
      } finally {
        Object.defineProperty(window, "location", {
          value: originalLocation,
          writable: true,
          configurable: true,
        });
      }
    }
  });

  test("renders the donation banner only on the home route", async () => {
    const originalLocation = window.location;
    Object.defineProperty(window, "location", {
      value: { pathname: "/", href: "" },
      writable: true,
      configurable: true,
    });

    try {
      const shell = renderShell();
      await shell.updateComplete;
      expect(shell.shadowRoot?.querySelector("donation-banner")).not.toBeNull();
      shell.remove();
    } finally {
      Object.defineProperty(window, "location", {
        value: originalLocation,
        writable: true,
        configurable: true,
      });
    }
  });

  test("hides the donation banner off the home route", async () => {
    const originalLocation = window.location;
    Object.defineProperty(window, "location", {
      value: { pathname: "/about", href: "" },
      writable: true,
      configurable: true,
    });

    try {
      const shell = renderShell();
      await shell.updateComplete;
      // The banner earns its keep on the landing page only; the footer
      // support line covers the rest of the site.
      expect(shell.shadowRoot?.querySelector("donation-banner")).toBeNull();
      shell.remove();
    } finally {
      Object.defineProperty(window, "location", {
        value: originalLocation,
        writable: true,
        configurable: true,
      });
    }
  });

  test("pin: the shell's shadow DOM stays flat and ordered on a non-home route", async () => {
    const originalLocation = window.location;
    Object.defineProperty(window, "location", {
      value: { pathname: "/about", href: "" },
      writable: true,
      configurable: true,
    });

    try {
      const shell = renderShell();
      await shell.updateComplete;

      // A broken start tag in the template parses the REST of the template
      // as children of <app-header> (which has no slot), so the strip,
      // main and the footer vanish from the page while every route test
      // still passes. Pin the direct-child order instead of looking
      // elements up by name anywhere in the tree.
      const tags = Array.from(shell.shadowRoot!.children).map((node) =>
        node.tagName.toLowerCase(),
      );
      expect(tags).toEqual([
        "a", // skip link
        "div", // .bg
        "app-header",
        "offline-banner",
        "main",
        "app-footer",
      ]);

      const header = shell.shadowRoot!.querySelector("app-header")!;
      expect(header.children.length).toBe(0);
      // Belt and braces: the count is the cheap check, this one names the
      // nodes that must never live inside the header.
      expect(
        header.querySelector("offline-banner, main, app-footer"),
      ).toBeNull();
      expect(shell.shadowRoot!.querySelector("main")!.parentNode).toBe(
        shell.shadowRoot,
      );
      shell.remove();
    } finally {
      Object.defineProperty(window, "location", {
        value: originalLocation,
        writable: true,
        configurable: true,
      });
    }
  });

  test("sets the catalog title and moves focus to main on navigation", async () => {
    const shell = renderShell();
    await shell.updateComplete;

    // pushState is mocked in this suite, so stub the pathname the shell
    // reads back for the title.
    const originalLocation = window.location;
    Object.defineProperty(window, "location", {
      value: { pathname: "/account", href: "" },
      writable: true,
      configurable: true,
    });

    try {
      navigate("/account");
      await shell.updateComplete;

      // SPA navigation does not reload the document — the shell manages
      // the title and focus so screen readers announce the new page.
      expect(document.title).toContain(el.titles.account);
      expect(shell.shadowRoot?.activeElement?.id).toBe("main-content");
    } finally {
      Object.defineProperty(window, "location", {
        value: originalLocation,
        writable: true,
        configurable: true,
      });
    }

    shell.remove();
  });
});
