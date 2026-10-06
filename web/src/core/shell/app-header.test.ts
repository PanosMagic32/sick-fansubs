import { describe, expect, test, beforeEach, afterEach } from "bun:test";

import "./app-header.js";
import "./app-shell.js";
import "@features/auth/data-access/session.js";
import { ApiError, NetworkError } from "@shared/api/client.js";
import { API_REQUEST_TIMEOUT_MS } from "@shared/api/request-signal.js";
import {
  stripCssComments,
  stripSourceComments,
  stubAbortSignalTimeout,
} from "@shared/api/test-utils.js";
import { el as catalog } from "@shared/catalog/el.js";
import { TRACKER_URL } from "@shared/config/links.js";
import { mapError } from "@shared/utils/format.js";

import type { AppHeader } from "./app-header.js";
import type { AppShell } from "./app-shell.js";
import type { SessionProvider } from "@features/auth/data-access/session.js";

// The stylesheet is read from disk rather than through the `?inline`
// import (it must not ride the bundler's CSS loader mapping); comments are
// stripped before matching, so prose can never decide a pin.
const headerStyles = stripCssComments(
  await Bun.file(new URL("./app-header.css", import.meta.url)).text(),
);

function renderHeader(): AppHeader {
  const el = document.createElement("app-header") as AppHeader;
  document.body.appendChild(el);
  return el;
}

/** A fetch mock routing by URL — the header's badge/profile fetches hit
 * different endpoints, so the tests need per-path responses. */
function mockFetch(
  handler: (url: string) => Response | Promise<Response>,
): void {
  globalThis.fetch = (async (input: RequestInfo | URL): Promise<Response> => {
    const url =
      typeof input === "string"
        ? input
        : input instanceof URL
          ? input.href
          : input.url;
    return handler(url);
  }) as unknown as typeof fetch;
}

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

/** Flush pending microtasks + one macrotask so async fetches settle. */
async function flush(): Promise<void> {
  await new Promise((r) => setTimeout(r, 10));
}

// Hygiene: a mid-test failure must not leak a stale header into later
// querySelector-based assertions; a mocked fetch is restored per test (one
// window is shared per worker).
let originalFetch: typeof globalThis.fetch | undefined;
let timeoutSpy: ReturnType<typeof stubAbortSignalTimeout> | null = null;
beforeEach(() => {
  originalFetch = globalThis.fetch;
  document.body.innerHTML = "";
  (globalThis.fetch as unknown) = undefined;
});
afterEach(() => {
  timeoutSpy?.restore();
  timeoutSpy = null;
  (globalThis.fetch as unknown) = originalFetch;
});

describe("app-header", () => {
  test("renders role banner", async () => {
    const el = renderHeader();
    await el.updateComplete;
    expect(el.getAttribute("role")).toBe("banner");
    el.remove();
  });

  test("renders brand link with site name from catalog", async () => {
    const el = renderHeader();
    await el.updateComplete;

    const brand = el.shadowRoot?.querySelector(".brand");
    expect(brand?.textContent?.trim()).toBe("Sick-Fansubs");
    expect(brand?.getAttribute("href")).toBe("/");

    el.remove();
  });

  test("renders navigation with aria-label", async () => {
    const el = renderHeader();
    await el.updateComplete;

    const nav = el.shadowRoot?.querySelector("nav");
    expect(nav?.getAttribute("aria-label")).toBe("Κύρια πλοήγηση");

    el.remove();
  });

  test("renders account link from catalog", async () => {
    const el = renderHeader();
    await el.updateComplete;

    const accountLink = el.shadowRoot?.querySelector(".header-right a");
    expect(accountLink?.textContent?.trim()).toContain("Λογαριασμός");

    el.remove();
  });

  test("hamburger button has aria-expanded", async () => {
    const el = renderHeader();
    await el.updateComplete;

    const btn = el.shadowRoot?.querySelector(
      ".hamburger",
    ) as HTMLElement | null;
    expect(btn?.getAttribute("aria-expanded")).toBe("false");

    el.remove();
  });

  test("hamburger toggle opens nav", async () => {
    const el = renderHeader();
    await el.updateComplete;

    const btn = el.shadowRoot?.querySelector(
      ".hamburger",
    ) as HTMLElement | null;
    btn?.click();
    await el.updateComplete;

    expect(btn?.getAttribute("aria-expanded")).toBe("true");
    const nav = el.shadowRoot?.querySelector("nav");
    expect(nav?.classList.contains("open")).toBe(true);

    el.remove();
  });

  test("Escape key closes open nav", async () => {
    const el = renderHeader();
    await el.updateComplete;

    // Open nav first.
    const btn = el.shadowRoot?.querySelector(
      ".hamburger",
    ) as HTMLElement | null;
    btn?.click();
    await el.updateComplete;

    // Press Escape.
    el.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    await el.updateComplete;

    const nav = el.shadowRoot?.querySelector("nav");
    expect(nav?.classList.contains("open")).toBe(false);

    el.remove();
  });

  test("outside click closes open nav", async () => {
    const el = renderHeader();
    await el.updateComplete;

    const btn = el.shadowRoot?.querySelector(
      ".hamburger",
    ) as HTMLElement | null;
    btn?.click();
    await el.updateComplete;
    expect(btn?.getAttribute("aria-expanded")).toBe("true");

    // Click somewhere outside the header (bubbles to document).
    const outside = document.createElement("div");
    document.body.appendChild(outside);
    outside.dispatchEvent(
      new MouseEvent("click", { bubbles: true, composed: true }),
    );
    await el.updateComplete;

    expect(btn?.getAttribute("aria-expanded")).toBe("false");
    expect(
      el.shadowRoot?.querySelector("nav")?.classList.contains("open"),
    ).toBe(false);

    outside.remove();
    el.remove();
  });

  test("click inside the header does not close open nav", async () => {
    const el = renderHeader();
    await el.updateComplete;

    const btn = el.shadowRoot?.querySelector(
      ".hamburger",
    ) as HTMLElement | null;
    btn?.click();
    await el.updateComplete;

    // A click on the PANEL itself (not a link, not the bar) is not an
    // outside click: the menu stays open. Every OTHER part of the header
    // bar closes it (see the suite below).
    const nav = el.shadowRoot?.querySelector(
      ".header-center",
    ) as HTMLElement | null;
    nav?.click();
    await el.updateComplete;

    expect(btn?.getAttribute("aria-expanded")).toBe("true");

    el.remove();
  });

  test("emits toggle-theme event on theme button click", async () => {
    const el = renderHeader();
    await el.updateComplete;

    let fired = false;
    el.addEventListener("toggle-theme", () => {
      fired = true;
    });

    const themeBtn = el.shadowRoot?.querySelector(
      ".theme-btn",
    ) as HTMLElement | null;
    themeBtn?.click();

    expect(fired).toBe(true);

    el.remove();
  });

  test("currentPath applies aria-current to matching link", async () => {
    const el = renderHeader();
    el.currentPath = "/search";
    await el.updateComplete;

    const links = el.shadowRoot?.querySelectorAll("nav a");
    const searchLink = [...(links ?? [])].find((a) =>
      a.textContent?.includes("Αναζήτηση"),
    );
    expect(searchLink?.getAttribute("aria-current")).toBe("page");

    el.remove();
  });

  test("Tracker link opens the external nyaa page with security attributes", async () => {
    const el = renderHeader();
    await el.updateComplete;

    const links = el.shadowRoot?.querySelectorAll("nav a");
    const trackerLink = [...(links ?? [])].find((a) =>
      a.textContent?.includes("Tracker"),
    );
    // The tracker link must navigate to the EXTERNAL nyaa user page —
    // never an internal SPA path.
    expect(trackerLink?.getAttribute("href")).toBe(TRACKER_URL);
    expect(trackerLink?.getAttribute("target")).toBe("_blank");
    expect(trackerLink?.getAttribute("rel")).toBe("noopener noreferrer");

    el.remove();
  });

  test("the account has ONE entry: the header chip, at every width", async () => {
    const el = renderHeader();
    await el.updateComplete;

    // The header chip is the single account entry; the dropdown carries
    // no account row.
    const navAccount = [
      ...(el.shadowRoot?.querySelectorAll("nav a") ?? []),
    ].filter(
      (a) =>
        a.getAttribute("href") === "/sign-in" ||
        a.getAttribute("href") === "/account",
    );
    expect(navAccount).toHaveLength(0);
    const chip = el.shadowRoot?.querySelector(".header-right a.account-link");
    expect(chip?.getAttribute("href")).toBe("/sign-in");

    el.remove();
  });

  test("account link keeps its label inside a responsive wrapper", async () => {
    const el = renderHeader();
    await el.updateComplete;

    const account = el.shadowRoot?.querySelector(
      ".header-right a.account-link",
    );
    expect(account?.querySelector(".link-text")?.textContent?.trim()).toBe(
      "Λογαριασμός",
    );

    el.remove();
  });

  test("account chip opens the account menu when authenticated", async () => {
    // NOTE: direct session injection is valid only OUTSIDE a provider tree
    // (no provider answers the @consume request, so the injected value is
    // never overwritten). The real provider → app-shell → app-header chain
    // is covered by the "real provider tree" test below.
    const el = renderHeader();
    (el as any).session = {
      state: {
        status: "authenticated",
        user: { id: "u1", username: "Katakuri", role: "user" },
      },
    };
    await el.updateComplete;

    const account = el.shadowRoot?.querySelector(
      ".header-right button.account-link",
    ) as HTMLButtonElement;
    expect(account).not.toBeNull();
    expect(account.querySelector(".link-text")?.textContent?.trim()).toBe(
      "Katakuri",
    );

    // The /account destination lives in the menu now — the chip is the
    // menu button.
    account.click();
    await el.updateComplete;
    expect(
      el.shadowRoot
        ?.querySelector(".account-menu a.account-menu-item")
        ?.getAttribute("href"),
    ).toBe("/account");

    // No second account entry in the nav: the chip above is the only one,
    // so a dropdown row cannot disagree with it.
    expect(el.shadowRoot?.querySelector("nav .mobile-only")).toBeNull();

    el.remove();
  });

  test("account link falls back to /sign-in while bootstrap is initializing", async () => {
    const el = renderHeader();
    (el as any).session = { state: { status: "initializing" } };
    await el.updateComplete;

    const account = el.shadowRoot?.querySelector(
      ".header-right a.account-link",
    );
    expect(account?.getAttribute("href")).toBe("/sign-in");
    expect(account?.querySelector(".link-text")?.textContent?.trim()).toBe(
      "Λογαριασμός",
    );

    el.remove();
  });

  test("the menu's account entry gets aria-current on the current route", async () => {
    const el = renderHeader();
    (el as any).session = {
      state: {
        status: "authenticated",
        user: { id: "u1", username: "Katakuri", role: "user" },
      },
    };
    el.currentPath = "/account";
    await el.updateComplete;

    (
      el.shadowRoot?.querySelector(".account-menu-button") as HTMLButtonElement
    ).click();
    await el.updateComplete;
    expect(
      el.shadowRoot
        ?.querySelector(".account-menu a.account-menu-item")
        ?.getAttribute("aria-current"),
    ).toBe("page");

    el.remove();
  });

  test("account link follows the session through the real provider tree", async () => {
    // The header consumes sessionContext through app-shell's shadow root
    // (session-provider → app-shell → app-header). This pins the
    // subscribe:true contract in the real topology: without it, a header
    // mounted before the bootstrap settles would never flip to /account.
    globalThis.fetch = (async () =>
      new Response(
        JSON.stringify({
          authenticated: true,
          csrfToken: "dummy",
          user: { id: "u1", username: "Katakuri", role: "user" },
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      )) as unknown as typeof fetch;

    const provider = document.createElement(
      "session-provider",
    ) as SessionProvider;
    const shell = document.createElement("app-shell") as AppShell;
    provider.appendChild(shell);
    document.body.appendChild(provider);

    await provider.updateComplete;
    await new Promise((r) => setTimeout(r, 10));
    await shell.updateComplete;

    const header = shell.shadowRoot?.querySelector(
      "app-header",
    ) as AppHeader | null;
    await header?.updateComplete;

    const account = header?.shadowRoot?.querySelector(
      ".header-right button.account-link",
    );
    // Disclosure, not a menu: aria-expanded alone carries the state.
    expect(account?.hasAttribute("aria-haspopup")).toBe(false);
    expect(account?.getAttribute("aria-expanded")).toBe("false");
    expect(account?.querySelector(".link-text")?.textContent?.trim()).toBe(
      "Katakuri",
    );
    // The sr-only suffix gives the bare username account context.
    expect(account?.textContent).toContain("Λογαριασμός");
  });

  test("nav labels stay in the DOM for the icon-only band", async () => {
    const el = renderHeader();
    await el.updateComplete;

    // Structural proxy: the actual hide/show contract lives in the CSS
    // (pinned by the "icon-only band pins the CSS contract" test). This
    // test guards that the labels exist to be hidden/announced at all.
    const links = el.shadowRoot?.querySelectorAll("nav a");
    const labels = [...(links ?? [])]
      .map((a) => a.querySelector(".link-text")?.textContent?.trim() ?? "")
      .filter((t) => t.length > 0);
    // Anglicism "Projects" (lang="en" scope; see the catalog comment on
    // el.nav.projects).
    expect(labels.some((t) => t.startsWith("Projects"))).toBe(true);
    expect(labels.some((t) => t.startsWith("Αναζήτηση"))).toBe(true);
    // The about link — Greek copy "Η ομάδα" (not an anglicism).
    expect(labels.some((t) => t.startsWith("Η ομάδα"))).toBe(true);
    // Tracker label includes the sr-only new-tab suffix.
    expect(labels.some((t) => t.startsWith("Tracker"))).toBe(true);
    // The account label lives on the HEADER CHIP only — it is not a nav
    // label any more.
    expect(labels.some((t) => t.startsWith("Λογαριασμός"))).toBe(false);

    el.remove();
  });

  test("nav order: tracker after an external divider, staff management last", async () => {
    const el = renderHeader();
    await el.updateComplete;

    const children = [...(el.shadowRoot?.querySelectorAll("nav > *") ?? [])];
    // Expected sequence (anonymous): projects → search → about (LAST among
    // the internal links, per colleague feedback) → divider → external
    // tracker. The account is NOT here (the header chip owns it) and the
    // staff-only management link (own divider, AFTER the tracker) is absent.
    const sequence = children.map((node) =>
      node.matches(".nav-divider")
        ? "divider"
        : (node.textContent ?? "").trim().slice(0, 20),
    );
    expect(sequence[0]).toContain("Projects");
    expect(sequence[1]).toContain("Αναζήτηση");
    expect(sequence[2]).toContain("Η ομάδα"); // about last, pre-divider
    expect(sequence[3]).toBe("divider");
    expect(sequence[4]).toContain("Tracker");
    expect(sequence.length).toBe(5);

    // The divider is decorative — hidden from assistive tech.
    const divider = children[3];
    expect(divider?.getAttribute("aria-hidden")).toBe("true");

    el.remove();
  });

  test("nav order for staff: management link after the tracker with its own divider", async () => {
    const el = renderHeader();
    (el as any).session = {
      state: {
        status: "authenticated",
        user: { id: "u1", username: "Katakuri", role: "moderator" },
      },
    };
    mockFetch(() => json({}));
    await el.updateComplete;
    await flush();

    const children = [...(el.shadowRoot?.querySelectorAll("nav > *") ?? [])];
    const sequence = children.map((node) =>
      node.matches(".nav-divider")
        ? "divider"
        : (node.textContent ?? "").trim().slice(0, 20),
    );
    // … about → divider → tracker → divider → Διαχείριση (the staff
    // management link sits AFTER the external link, behind its own
    // divider — a discreet control, not one more internal nav item).
    expect(sequence[2]).toContain("Η ομάδα");
    expect(sequence[3]).toBe("divider");
    expect(sequence[4]).toContain("Tracker");
    expect(sequence[5]).toBe("divider");
    expect(sequence[6]).toContain("Διαχείριση");
    expect(sequence.length).toBe(7);

    const adminLink = children[6] as HTMLAnchorElement;
    expect(adminLink.getAttribute("href")).toBe("/admin");
    // The sliders GLYPH renders as an actual SVG path (the markup
    // contract — same pin as the bell's).
    expect(adminLink.querySelector("svg path")).not.toBeNull();

    el.remove();
  });

  test("pill geometry is reserved on every nav link (no jump on selection)", () => {
    // happy-dom cannot measure layout, so the no-jump contract is pinned
    // against the CSS source: the BASE link rule carries the pill's
    // padding + radius, and the ACTIVE rule changes only colors — the
    // active state can never resize an element.
    const base = headerStyles.match(/\.header-center a \{([^}]*)\}/s)?.[1];
    expect(base).toContain("padding: 4px 14px");
    expect(base).toContain("border-radius: 999px");

    const active = headerStyles.match(
      /\.header-center a\[aria-current="page"\] \{([^}]*)\}/s,
    )?.[1];
    expect(active).toContain("background: var(--sf-accent)");
    expect(active).not.toContain("padding");
    expect(active).not.toContain("border-radius");

    // The divider: vertical on desktop/band, horizontal in the dropdown.
    // The band is extracted whole first, then the rule body — no scan may
    // reach into a later rule.
    expect(headerStyles).toMatch(/\.nav-divider \{[^}]*width: 1px/s);
    const phone =
      headerStyles.match(/@media \(max-width: 700px\) \{(.*?)\n\}/s)?.[1] ?? "";
    const phoneDivider = phone.match(/\.nav-divider \{([^}]*)\}/)?.[1] ?? "";
    expect(phoneDivider).toContain("width: 100%");
  });

  test("dropdown entries read as buttons, and the panel drops the route highlight", async () => {
    // happy-dom cannot compute styles; the button look is pinned at its
    // source — the shared vocabulary classes on the panel's entries — plus
    // the layout-only local rule and the scoped aria-current selector.
    const source = stripSourceComments(
      await Bun.file(new URL("./app-header.ts", import.meta.url)).text(),
    );
    expect(source).toContain(
      'class="button button--secondary account-menu-item"',
    );
    expect(source).toContain('class="button button--danger account-menu-item"');
    expect(source).not.toContain("account-menu-item--danger");

    const css = headerStyles;
    expect(css).toMatch(
      /\.header-right a\[aria-current="page"\]:not\(\.account-menu-item\)/,
    );
    // No unscoped rule may survive — the panel's route highlight stays gone.
    expect(css).not.toMatch(/\.header-right a\[aria-current="page"\] \{/);

    const item = css.match(/\.account-menu-item \{([^}]*)\}/s)?.[1] ?? "";
    expect(item).toContain("width: 100%");
    expect(item).toContain("min-height: 44px");

    // The ≤700px nav dropdown gives its links the same button frame.
    const phone =
      css.match(/@media \(max-width: 700px\) \{([\s\S]*?)\n\}/s)?.[1] ?? "";
    expect(phone).toMatch(/\.header-center a \{[^}]*border-radius: 10px/s);
    expect(phone).toMatch(
      /\.header-center a \{[^}]*background: var\(--sf-surface\)/s,
    );
  });

  test("nav links align icon and label structurally (flex, not baseline magic)", () => {
    // happy-dom cannot measure layout, so the alignment contract is pinned
    // against the CSS source: the desktop tier aligns glyph + label on one
    // flex line with an explicit gap (vertical-align + collapsed-whitespace
    // spacing drifted by font). The mobile-only escape hatch is gone with
    // the duplicate account row, so the rule covers every nav link.
    const rules = [
      ...headerStyles.matchAll(/\.header-center a \{([^}]*)\}/gs),
    ].map((m) => m[1] ?? "");
    const rule = rules.find((body) => body.includes("display: flex")) ?? "";
    expect(rule).toContain("display: flex");
    expect(rule).toContain("align-items: center");
    expect(rule).toContain("gap: 6px");
  });

  test("Tracker announces new-tab for screen readers", async () => {
    const el = renderHeader();
    await el.updateComplete;

    const links = el.shadowRoot?.querySelectorAll("nav a");
    const trackerLink = [...(links ?? [])].find((a) =>
      a.textContent?.includes("Tracker"),
    );
    expect(trackerLink?.querySelector(".sr-only")?.textContent?.trim()).toBe(
      "(ανοίγει σε νέα καρτέλα)",
    );

    el.remove();
  });

  test("icon-only band pins the CSS contract (clip, not display:none)", () => {
    // happy-dom cannot evaluate media queries, so the a11y contract is
    // pinned against the CSS source itself. These assertions check the
    // hiding MECHANISM, not just selector presence: labels must be
    // clipped (never display:none), the account chip must stay visible at
    // every width (the single account entry), and the external arrow must be
    // display:none in the band.

    // The duplicate account row is gone: no .mobile-only exists any more,
    // neither as markup nor as a rule (a leftover rule would be dead code
    // the moment the class left the template).
    expect(headerStyles).not.toContain(".mobile-only");

    // Band (700.01–1300px): labels clipped, external arrow hidden.
    const band = headerStyles.match(
      /@media \(min-width: 700\.01px\) and \(max-width: 1300px\) \{(.*?)\n\}/s,
    );
    expect(band).not.toBeNull();
    const body = band?.[1] ?? "";

    const labelRule =
      body.match(/\.header-center \.link-text \{([^}]*)\}/s)?.[1] ?? "";
    expect(labelRule).toContain("clip: rect(0, 0, 0, 0)");
    expect(labelRule).not.toContain("display");
    expect(body).toMatch(/\.external-icon \{[^}]*display: none/s);

    // Mobile (≤700px): NO account-LINK rule — the chip is the one account
    // entry at every width (a hide rule would also be defeated by a
    // specificity inversion: chip and dropdown row would show at once).
    // Comment-blind, non-vacuous (the block must resolve and be the real
    // one), and it forbids ANY account-link selector rather than one spelling
    // of a hide rule; the account MENU's phone-band sheet is pinned
    // separately.
    const mobile =
      headerStyles.match(/@media \(max-width: 700px\) \{(.*?)\n\}/s)?.[1] ?? "";
    expect(mobile).toContain(".hamburger");
    expect(mobile).not.toContain("account-link");

    // The account/bell labels are clipped below the ultrawide tier (the
    // icon-only right-side links) — pin the BASE rule:
    // the clip mechanism, never display:none, so the accessible name is
    // unchanged.
    const base = headerStyles.match(
      /\.header-right \.link-text \{([^}]*)\}/s,
    )?.[1];
    expect(base).toContain("clip: rect(0, 0, 0, 0)");
    expect(base).not.toContain("display");
    // The clip selector must stay ELEMENT-AGNOSTIC: an `a`-qualified rule
    // exempts every non-anchor chip, and the signed-in account chip is a
    // <button> — its username leaked onto mobile exactly that way.
    expect(headerStyles).not.toMatch(/\.header-right a \.link-text \{/);
    expect(headerStyles).not.toMatch(/\.header-right button \.link-text \{/);

    // Ultrawide tier (>1920px, bigger than FHD): the
    // labels are REVEALED by un-clipping (position/width/overflow/clip
    // reset), never by a display flip — the same mechanism in reverse.
    const ultrawide = headerStyles.match(
      /@media \(min-width: 1920\.01px\) \{(.*?)\n\}/s,
    );
    expect(ultrawide).not.toBeNull();
    const ultra = ultrawide?.[1] ?? "";
    expect(ultra).toContain("clip: auto");
    expect(ultra).toContain("position: static");
    expect(ultra).toContain(".header-right .link-text");
    expect(ultra).not.toMatch(/\.header-right a \.link-text/);
    expect(ultra).not.toMatch(/display/);

    // The account link and the theme toggle share ONE footprint: a 40px
    // circle. The avatar chip is a 40px circle with its letter at the
    // uniform 1.5rem size (matching the toggle's glyph), and the
    // anonymous default icon keeps the 1.5rem glyph centered in the same
    // 40px box — the link's footprint never changes across auth states.
    // Each rule body is extracted whole, so a match cannot reach a later
    // rule.
    const accountRule =
      headerStyles.match(
        /\.header-right a\.account-link,\s*\.header-right button\.account-link \{([^}]*)\}/s,
      )?.[1] ?? "";
    expect(accountRule).toContain("min-width: 40px");
    expect(accountRule).toContain("min-height: 40px");
    const avatarRule =
      headerStyles.match(
        /\.header-right a\.account-link \.avatar,\s*\.header-right button\.account-link \.avatar \{([^}]*)\}/s,
      )?.[1] ?? "";
    expect(avatarRule).toContain("width: 40px");
    expect(avatarRule).toContain("height: 40px");
    expect(avatarRule).toContain("font-size: 1.5rem");
    const svgRule =
      headerStyles.match(
        /\.header-right a\.account-link > svg,\s*\.header-right button\.account-link > svg \{([^}]*)\}/s,
      )?.[1] ?? "";
    expect(svgRule).toContain("width: 1.5rem");
    expect(svgRule).toContain("height: 1.5rem");
    // The bell keeps the house icon-target floor (2rem); the account chip
    // and toggle are 40px, so a bare glyph box would be the lone outlier.
    const bellRule =
      headerStyles.match(/\.header-right a\.bell-link \{([^}]*)\}/)?.[1] ?? "";
    expect(bellRule).toContain("min-width: 2rem");
    expect(bellRule).toContain("min-height: 2rem");
  });

  test("the mobile dropdown is an opaque, bordered panel", () => {
    // happy-dom cannot evaluate media queries, so the panel chrome is
    // pinned against the CSS source (comments are stripped upstream, so
    // prose can never decide a `not.toContain`).
    const source = headerStyles;
    const mobile =
      source.match(/@media \(max-width: 700px\) \{(.*?)\n\}/s)?.[1] ?? "";
    const panel = mobile.match(/\.header-center \{([^}]*)\}/)?.[1] ?? "";
    // OPAQUE: a translucent fill let the page show through under the open
    // menu. The panel therefore takes the solid page background and must
    // NOT carry a translucent token or a blur.
    expect(panel).toContain("background: var(--sf-bg)");
    expect(panel).not.toContain("var(--sf-surface)");
    expect(panel).not.toContain("backdrop-filter");
    // The BORDER is what distinguishes the panel, so it must be
    // DISTINCTIVE: the 0.16-alpha --sf-border was reported invisible.
    expect(panel).toContain("border: 1px solid var(--sf-text-dim)");
    expect(panel).toContain("border-radius: 16px");
    expect(panel).toContain("box-shadow:");
    expect(panel).not.toContain("border-bottom: 1px solid var(--sf-border)");
  });

  test("every header icon shares one 1.5rem size", () => {
    // happy-dom cannot measure layout, so the uniform-icon contract is
    // pinned against the CSS source: the shared anchor rule sizes every
    // svg at 1.5rem, and the theme toggle joins via its own rule (icons
    // must not vary per link).
    const anchorRule = headerStyles.match(/^a svg \{([^}]*)\}/ms)?.[1];
    expect(anchorRule).toContain("width: 1.5rem");
    expect(anchorRule).toContain("height: 1.5rem");

    const themeRule = headerStyles.match(/\.theme-btn svg \{([^}]*)\}/s)?.[1];
    expect(themeRule).toContain("width: 1.5rem");
    expect(themeRule).toContain("height: 1.5rem");
  });

  test("bell renders for an authenticated session and carries the unread badge", async () => {
    const el = renderHeader();
    (el as any).session = {
      state: {
        status: "authenticated",
        user: { id: "u1", username: "Katakuri", role: "moderator" },
      },
    };
    mockFetch((url) => {
      if (url.includes("/notifications/unread-count"))
        return json({ unread: 3 });
      if (url.includes("/users/me")) {
        return json({
          id: "u1",
          username: "Katakuri",
          email: "k@example.com",
          role: "moderator",
          createdAt: "2026-01-01T00:00:00.000Z",
          avatarUrl: null,
        });
      }
      return json({});
    });
    await el.updateComplete;
    await flush();

    const bell = el.shadowRoot?.querySelector(".header-right a.bell-link");
    expect(bell).not.toBeNull();
    expect(bell?.getAttribute("href")).toBe("/notifications");
    // The bell GLYPH renders as an actual SVG path (the markup contract —
    // a missing/broken path would leave an invisible 4px link).
    expect(bell?.querySelector("svg path")).not.toBeNull();
    // The label is present in the DOM for the ultrawide reveal (clipped
    // below 1920px by the base rule, never display:none).
    expect(bell?.querySelector(".link-text")?.textContent?.trim()).toBe(
      "Ειδοποιήσεις",
    );
    const badge = bell?.querySelector(".unread-badge");
    // The badge belongs to the GLYPH wrapper; on the label it would escape
    // the positioning context and drift under the ultrawide reveal.
    expect(badge?.parentElement?.classList.contains("bell-icon")).toBe(true);
    expect(badge?.textContent?.trim()).toBe("3");
    // The badge count joins the accessible name via the catalog plural.
    expect(bell?.getAttribute("aria-label")).toBe(
      "Ειδοποιήσεις, 3 αδιάβαστες ειδοποιήσεις",
    );

    el.remove();
  });

  test("badge and avatar refreshes ride the shared client deadline", async () => {
    timeoutSpy = stubAbortSignalTimeout();
    const el = renderHeader();
    (el as any).session = {
      state: {
        status: "authenticated",
        user: { id: "u1", username: "Katakuri", role: "moderator" },
      },
    };
    mockFetch((url) => {
      if (url.includes("/notifications/unread-count"))
        return json({ unread: 0 });
      return json({
        id: "u1",
        username: "Katakuri",
        email: "k@example.com",
        role: "moderator",
        createdAt: "2026-01-01T00:00:00.000Z",
        avatarUrl: null,
      });
    });
    await el.updateComplete;
    await flush();

    // Both auxiliary reads compose the page controller with the client
    // deadline (requestSignal) — a hung request must not outlive it.
    expect(timeoutSpy.spy.mock.calls.length).toBeGreaterThanOrEqual(2);
    for (const call of timeoutSpy.spy.mock.calls) {
      expect(call[0]).toBe(API_REQUEST_TIMEOUT_MS);
    }
    el.remove();
  });

  test("bell is hidden for anonymous sessions and shown for plain users", async () => {
    const anonymous = renderHeader();
    await anonymous.updateComplete;
    expect(anonymous.shadowRoot?.querySelector(".bell-link")).toBeNull();
    anonymous.remove();

    // A plain (non-staff) authenticated user gets the bell too — the feed
    // unification moved the floor to any signed-in session.
    const plainUser = renderHeader();
    (plainUser as any).session = {
      state: {
        status: "authenticated",
        user: { id: "u1", username: "Katakuri", role: "user" },
      },
    };
    mockFetch((url) => {
      if (url.includes("/users/me")) {
        return json({
          id: "u1",
          username: "Katakuri",
          email: "k@example.com",
          role: "user",
          createdAt: "2026-01-01T00:00:00.000Z",
          avatarUrl: null,
        });
      }
      return json({});
    });
    await plainUser.updateComplete;
    await flush();
    expect(plainUser.shadowRoot?.querySelector(".bell-link")).not.toBeNull();
    plainUser.remove();
  });

  test("account link shows the avatar image when the profile has one", async () => {
    const el = renderHeader();
    (el as any).session = {
      state: {
        status: "authenticated",
        user: { id: "u1", username: "Katakuri", role: "user" },
      },
    };
    mockFetch((url) => {
      if (url.includes("/users/me")) {
        return json({
          id: "u1",
          username: "Katakuri",
          email: "k@example.com",
          role: "user",
          createdAt: "2026-01-01T00:00:00.000Z",
          avatarUrl:
            "http://localhost:3000/media/images/ab1234567890abcdef1234567890abcd.png",
        });
      }
      return json({});
    });
    await el.updateComplete;
    await flush();

    const account = el.shadowRoot?.querySelector(
      ".header-right button.account-link",
    );
    expect(account?.querySelector("img.avatar")?.getAttribute("src")).toBe(
      "http://localhost:3000/media/images/ab1234567890abcdef1234567890abcd.png",
    );
    el.remove();
  });

  test("account link falls back to the initial letter without an avatar", async () => {
    const el = renderHeader();
    (el as any).session = {
      state: {
        status: "authenticated",
        user: { id: "u1", username: "Katakuri", role: "user" },
      },
    };
    mockFetch((url) => {
      if (url.includes("/users/me")) {
        return json({
          id: "u1",
          username: "Katakuri",
          email: "k@example.com",
          role: "user",
          createdAt: "2026-01-01T00:00:00.000Z",
          avatarUrl: null,
        });
      }
      return json({});
    });
    await el.updateComplete;
    await flush();

    const account = el.shadowRoot?.querySelector(
      ".header-right button.account-link",
    );
    expect(account?.querySelector(".avatar-initial")?.textContent?.trim()).toBe(
      "K",
    );
    expect(account?.querySelector("img.avatar")).toBeNull();
    el.remove();
  });

  test("account link shows the default user icon when anonymous", async () => {
    const el = renderHeader();
    await el.updateComplete;

    const account = el.shadowRoot?.querySelector(
      ".header-right a.account-link",
    );
    expect(account?.querySelector("svg")).not.toBeNull();
    expect(account?.querySelector(".avatar")).toBeNull();
    el.remove();
  });

  test("the avatar-change event refetches the profile for the chip", async () => {
    const el = renderHeader();
    (el as any).session = {
      state: {
        status: "authenticated",
        user: { id: "u1", username: "Katakuri", role: "user" },
      },
    };

    let profileCalls = 0;
    mockFetch((url) => {
      if (url.includes("/users/me")) {
        profileCalls++;
        return json({
          id: "u1",
          username: "Katakuri",
          email: "k@example.com",
          role: "user",
          createdAt: "2026-01-01T00:00:00.000Z",
          // The first fetch (mount) has no avatar; the post-event fetch
          // returns the fresh ABSOLUTE served URL.
          avatarUrl:
            profileCalls === 1
              ? null
              : "http://localhost:3000/media/images/ab1234567890abcdef1234567890abcd.png",
        });
      }
      return json({});
    });
    await el.updateComplete;
    await flush();

    // Initial state: the initial-letter chip.
    const account = el.shadowRoot?.querySelector(
      ".header-right button.account-link",
    );
    expect(account?.querySelector(".avatar-initial")).not.toBeNull();

    // The /account page dispatches this after a CONFIRMED upload success.
    window.dispatchEvent(new CustomEvent("sf-avatar-changed"));
    await flush();
    await el.updateComplete;

    expect(profileCalls).toBeGreaterThanOrEqual(2);
    expect(account?.querySelector("img.avatar")?.getAttribute("src")).toBe(
      "http://localhost:3000/media/images/ab1234567890abcdef1234567890abcd.png",
    );
    el.remove();
  });
});

describe("app-header mobile dropdown closes", () => {
  function renderHeaderWithNav(): AppHeader {
    const el = document.createElement("app-header") as AppHeader;
    document.body.appendChild(el);
    return el;
  }

  test("the brand link closes the open menu — it navigates home from INSIDE the header", async () => {
    // With the dropdown open, tapping the brand must navigate home AND
    // close the panel. The outside-click listener cannot see it (the brand
    // is part of the header), so the brand carries the close handler
    // explicitly.
    const el = renderHeaderWithNav();
    el.navOpen = true;
    await el.updateComplete;

    const brand = el.shadowRoot?.querySelector(".brand") as HTMLAnchorElement;
    brand.dispatchEvent(
      new MouseEvent("click", { bubbles: true, composed: true }),
    );
    await el.updateComplete;

    expect(el.navOpen).toBe(false);
    el.remove();
  });

  test("any navigation closes the menu, whoever started it", async () => {
    // The second half of the same bug: the menu must follow the ROUTE, not
    // only direct clicks on its own links — a programmatic navigate, an
    // in-page control, or back/forward all leave the panel behind.
    const el = renderHeaderWithNav();
    el.navOpen = true;
    await el.updateComplete;

    el.currentPath = "/projects";
    await el.updateComplete;

    expect(el.navOpen).toBe(false);
    el.remove();
  });

  test("a re-render that does NOT change the path leaves the menu alone", async () => {
    const el = renderHeaderWithNav();
    // Mount settles first: Lit reports every property as changed on the
    // FIRST update, and that mount update must not count as a navigation
    // (it once closed a menu the header had just been asked to show).
    await el.updateComplete;

    el.navOpen = true;
    await el.updateComplete;
    el.theme = "light";
    await el.updateComplete;
    expect(el.navOpen).toBe(true); // a badge refresh / theme flip never fights the user

    el.currentPath = "/projects";
    await el.updateComplete;
    expect(el.navOpen).toBe(false); // a real navigation still closes it
    el.remove();
  });
});

describe("app-header — clicking the header closes the open menu", () => {
  function mount(): AppHeader {
    const el = document.createElement("app-header") as AppHeader;
    document.body.appendChild(el);
    return el;
  }

  test("the shadow-root click listener survives a reconnect", async () => {
    const el = mount();
    await el.updateComplete;

    (el.shadowRoot!.querySelector(".hamburger") as HTMLElement).click();
    await el.updateComplete;
    expect(el.navOpen).toBe(true);

    el.remove();
    document.body.appendChild(el);
    await el.updateComplete;

    (el.shadowRoot!.querySelector(".theme-btn") as HTMLElement).click();
    await el.updateComplete;
    expect(el.navOpen).toBe(false);
    el.remove();
  });

  test("a tap on the header bar (theme toggle) closes the menu", async () => {
    // Tapping the bar around the panel must close it, not leave it open.
    const el = mount();
    el.navOpen = true;
    await el.updateComplete;

    const theme = el.shadowRoot?.querySelector(".theme-btn") as HTMLElement;
    theme.dispatchEvent(
      new MouseEvent("click", { bubbles: true, composed: true }),
    );
    await el.updateComplete;

    expect(el.navOpen).toBe(false);
    el.remove();
  });

  test("a tap on the brand area closes the menu", async () => {
    const el = mount();
    el.navOpen = true;
    await el.updateComplete;

    const left = el.shadowRoot?.querySelector(".header-left") as HTMLElement;
    left.dispatchEvent(
      new MouseEvent("click", { bubbles: true, composed: true }),
    );
    await el.updateComplete;

    expect(el.navOpen).toBe(false);
    el.remove();
  });

  test("a tap INSIDE the panel keeps it open, and the hamburger still toggles", async () => {
    const el = mount();
    el.navOpen = true;
    await el.updateComplete;

    const nav = el.shadowRoot?.querySelector(".header-center") as HTMLElement;
    // A click on the panel's own padding (not a link): the menu stays. The
    // assertion that makes this a DISCRIMINATING pin is the follow-up tap —
    // "still open" alone would also pass with no listener at all, but a bar
    // tap after the panel tap only closes if the panel click was genuinely
    // treated as inside.
    nav.dispatchEvent(
      new MouseEvent("click", { bubbles: true, composed: true }),
    );
    await el.updateComplete;
    expect(el.navOpen).toBe(true);

    const theme = el.shadowRoot?.querySelector(".theme-btn") as HTMLElement;
    theme.dispatchEvent(
      new MouseEvent("click", { bubbles: true, composed: true }),
    );
    await el.updateComplete;
    expect(el.navOpen).toBe(false);

    // The hamburger still toggles (the one inner control that must not be
    // treated as "outside the panel").
    el.navOpen = true;
    await el.updateComplete;
    const hamburger = el.shadowRoot?.querySelector(".hamburger") as HTMLElement;
    hamburger.dispatchEvent(
      new MouseEvent("click", { bubbles: true, composed: true }),
    );
    await el.updateComplete;
    expect(el.navOpen).toBe(false);
    el.remove();
  });
});

describe("app-header — the signed-in account menu", () => {
  interface SignOutMocks {
    signOut?: () => Promise<void>;
    signOutAll?: () => Promise<void>;
    revalidate?: () => Promise<void>;
  }

  function mountAuthed(mocks: SignOutMocks = {}): AppHeader {
    const el = renderHeader();
    (el as any).session = {
      state: {
        status: "authenticated",
        user: { id: "u1", username: "Katakuri", role: "user" },
      },
      signOut: mocks.signOut ?? (async () => {}),
      signOutAll: mocks.signOutAll ?? (async () => {}),
      revalidate: mocks.revalidate ?? (async () => {}),
    };
    return el;
  }

  function open(el: AppHeader): void {
    (
      el.shadowRoot?.querySelector(".account-menu-button") as HTMLButtonElement
    ).click();
  }

  function itemButtons(el: AppHeader): HTMLButtonElement[] {
    return [
      ...(el.shadowRoot?.querySelectorAll(
        ".account-menu button.account-menu-item",
      ) ?? []),
    ] as HTMLButtonElement[];
  }

  test("signed out the brand stays the home link and the chip stays the /sign-in link", async () => {
    const el = renderHeader();
    await el.updateComplete;
    const brand = el.shadowRoot?.querySelector(".brand") as HTMLAnchorElement;
    expect(brand.localName).toBe("a");
    expect(brand.getAttribute("href")).toBe("/");
    const chip = el.shadowRoot?.querySelector(
      ".header-right a.account-link",
    ) as HTMLAnchorElement;
    expect(chip.getAttribute("href")).toBe("/sign-in");
    expect(el.shadowRoot?.querySelector(".account-menu-button")).toBeNull();
    expect(el.shadowRoot?.querySelector(".account-menu")).toBeNull();
    el.remove();
  });

  test("signed in the chip is a disclosure button; the menu holds the account link plus both sign-out actions", async () => {
    const el = mountAuthed();
    await el.updateComplete;

    // The brand stays the home link in both states (owner: the menu lives on
    // the avatar chip).
    const brand = el.shadowRoot?.querySelector(".brand") as HTMLAnchorElement;
    expect(brand.localName).toBe("a");
    expect(brand.getAttribute("href")).toBe("/");

    const chip = el.shadowRoot?.querySelector(
      ".account-menu-button",
    ) as HTMLButtonElement;
    expect(chip.getAttribute("aria-expanded")).toBe("false");
    expect(el.shadowRoot?.querySelector(".account-menu")).toBeNull();

    open(el);
    await el.updateComplete;
    expect(chip.getAttribute("aria-expanded")).toBe("true");

    const menu = el.shadowRoot?.querySelector(".account-menu") as HTMLElement;
    // A disclosure panel, not an ARIA menu (no roving tabindex contract).
    expect(menu.getAttribute("role")).toBeNull();
    const account = menu.querySelector(
      "a.account-menu-item",
    ) as HTMLAnchorElement;
    expect(account.getAttribute("href")).toBe("/account");
    expect(account.textContent?.trim()).toBe(catalog.ui.account);
    expect(itemButtons(el).map((b) => b.textContent?.trim())).toEqual([
      catalog.ui.signOut,
      catalog.auth.signOutAll,
    ]);
    el.remove();
  });

  test("sign out calls the session and closes the menu; sign-out-all is its own action", async () => {
    const calls: string[] = [];
    const el = mountAuthed({
      signOut: async () => {
        calls.push("out");
      },
      signOutAll: async () => {
        calls.push("all");
      },
    });
    await el.updateComplete;

    open(el);
    await el.updateComplete;
    // The keyboard user's control: the panel closes around it, so focus
    // must return to the chip (no route change owns focus here).
    const signOutBtn = itemButtons(el)[0]!;
    signOutBtn.focus();
    signOutBtn.click();
    await flush();
    await el.updateComplete;
    expect(calls).toEqual(["out"]);
    expect(el.shadowRoot?.querySelector(".account-menu")).toBeNull();
    expect(el.shadowRoot?.activeElement).toBe(
      el.shadowRoot?.querySelector(".account-link"),
    );

    open(el);
    await el.updateComplete;
    itemButtons(el)[1]!.click();
    await flush();
    await el.updateComplete;
    expect(calls).toEqual(["out", "all"]);
    el.remove();
  });

  test("a failed sign-out keeps the menu open and shows the mapped message", async () => {
    const err = new NetworkError("offline");
    const el = mountAuthed({
      signOut: async () => {
        throw err;
      },
    });
    await el.updateComplete;
    open(el);
    await el.updateComplete;
    itemButtons(el)[0]!.click();
    await flush();
    await el.updateComplete;

    const alert = el.shadowRoot?.querySelector(
      ".account-menu-error",
    ) as HTMLElement;
    expect(alert).not.toBeNull();
    expect(alert.getAttribute("role")).toBe("alert");
    expect(alert.textContent?.trim()).toBe(mapError(err));
    expect(itemButtons(el)[0]!.disabled).toBe(false);
    el.remove();
  });

  test("a 403 sign-out revalidates to heal the stale CSRF token", async () => {
    let revalidated = 0;
    const el = mountAuthed({
      signOut: async () => {
        throw new ApiError({
          type: "/problems/forbidden",
          title: "Forbidden",
          status: 403,
        });
      },
      revalidate: async () => {
        revalidated++;
      },
    });
    await el.updateComplete;
    open(el);
    await el.updateComplete;
    itemButtons(el)[0]!.click();
    await flush();
    await el.updateComplete;
    expect(revalidated).toBe(1);
    el.remove();
  });

  test("Escape closes the menu and returns focus to the account chip", async () => {
    const el = mountAuthed();
    await el.updateComplete;
    open(el);
    await el.updateComplete;
    el.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
    );
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector(".account-menu")).toBeNull();
    expect((el.shadowRoot as any)?.activeElement).toBe(
      el.shadowRoot?.querySelector(".account-menu-button"),
    );
    el.remove();
  });

  test("an outside click closes the menu; a click inside it does not", async () => {
    const el = mountAuthed();
    await el.updateComplete;
    open(el);
    await el.updateComplete;

    const menu = el.shadowRoot?.querySelector(".account-menu") as HTMLElement;
    menu.dispatchEvent(
      new MouseEvent("click", { bubbles: true, composed: true }),
    );
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector(".account-menu")).not.toBeNull();

    document.dispatchEvent(
      new MouseEvent("click", { bubbles: true, composed: true }),
    );
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector(".account-menu")).toBeNull();
    el.remove();
  });

  test("a click on another header control closes the menu", async () => {
    const el = mountAuthed();
    await el.updateComplete;
    open(el);
    await el.updateComplete;

    const theme = el.shadowRoot?.querySelector(".theme-btn") as HTMLElement;
    theme.dispatchEvent(
      new MouseEvent("click", { bubbles: true, composed: true }),
    );
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector(".account-menu")).toBeNull();
    el.remove();
  });

  test("a session flip resets the menu — a re-sign-in starts closed", async () => {
    const el = mountAuthed();
    await el.updateComplete;
    open(el);
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector(".account-menu")).not.toBeNull();

    // Any session change that leaves the authenticated state must clear the
    // panel state, so the next sign-in cannot resurrect it.
    (el as any).session = {
      state: { status: "anonymous" },
      signOut: async () => {},
      signOutAll: async () => {},
      revalidate: async () => {},
    };
    el.requestUpdate();
    await el.updateComplete;
    (el as any).session = {
      state: {
        status: "authenticated",
        user: { id: "u1", username: "Katakuri", role: "user" },
      },
      signOut: async () => {},
      signOutAll: async () => {},
      revalidate: async () => {},
    };
    el.requestUpdate();
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector(".account-menu")).toBeNull();
    el.remove();
  });

  test("a navigation closes the menu", async () => {
    const el = mountAuthed();
    await el.updateComplete;
    open(el);
    await el.updateComplete;
    el.currentPath = "/projects";
    await el.updateComplete;
    // The close lands in the updated() of the path change, which schedules
    // one more render — settle it before querying the DOM.
    await flush();
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector(".account-menu")).toBeNull();
    el.remove();
  });

  test("the phone band turns the panel into a full-width sheet (raw-sheet pin)", () => {
    // happy-dom cannot evaluate media queries. Right-anchored to a chip that
    // sits near the right edge, the 15rem panel runs off the LEFT of a phone
    // viewport — the band pins it to the bar like the nav dropdown instead.
    const source = headerStyles;
    const mobile =
      source.match(/@media \(max-width: 700px\) \{(.*?)\n\}/s)?.[1] ?? "";
    const root = mobile.match(/\.account-menu-root \{([^}]*)\}/)?.[1] ?? "";
    expect(root).toContain("position: static");
    const panel = mobile.match(/\.account-menu \{([^}]*)\}/)?.[1] ?? "";
    expect(panel).toContain("top: 64px");
    expect(panel).toContain("left: 0");
    expect(panel).toContain("right: 0");
  });
});
