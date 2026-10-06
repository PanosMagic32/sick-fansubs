import {
  describe,
  expect,
  test,
  beforeEach,
  afterEach,
  mock,
  spyOn,
} from "bun:test";

import {
  ApiError,
  NetworkError,
  RequestTimeoutError,
  ValidationError,
  clearCSRFToken,
  setCSRFToken,
} from "@shared/api/client.js";
import {
  mockFetchImpl,
  mockResponse,
  mockSessionContext,
  listenForNavigation,
  restoreMockFetch,
  stripCssComments,
  stubProperty,
  waitFor,
} from "@shared/api/test-utils.js";
import { el } from "@shared/catalog/el.js";

import "@features/auth/data-access/session.js";
import "./account-page.js";
import type { AccountPage } from "./account-page.js";
import type { ContentCard } from "@shared/ui/content-card.js";
import type { SessionProvider } from "@features/auth/data-access/session.js";

// The stylesheet is read from disk rather than through `?inline`, so the
// geometry pins assert the source (comment-stripped, so prose can never
// satisfy a rule pin — same pattern as app-shell.test.ts).
const accountStyles = stripCssComments(
  await Bun.file(new URL("./account-page.css", import.meta.url)).text(),
);

// ── Helpers ─────────────────────────────────────────────────────────

/**
 * Create a mock SessionContext. Injection is valid only OUTSIDE a provider
 * tree (no provider answers the @consume request, so the injected value is
 * never overwritten). The state object is static — tests that need the
 * session to FLIP state (e.g. sign-out → anonymous) are covered by the
 * guard test here plus the provider's own state-machine tests.
 */
function mockSession(
  overrides: {
    status?: "initializing" | "anonymous" | "authenticated" | "error";
    message?: string;
    mustChangePassword?: boolean;
    changePassword?: (cur: string, next: string) => Promise<void>;
    signOut?: () => Promise<void>;
    signOutAll?: () => Promise<void>;
    retryBootstrap?: () => void;
    revalidate?: () => void | Promise<void>;
  } = {},
) {
  return mockSessionContext({ ...overrides, user: { username: "Katakuri" } });
}

function render(
  overrides: Parameters<typeof mockSession>[0] = {},
): AccountPage {
  const page = document.createElement("account-page") as AccountPage;
  (page as any).session = mockSession(overrides);
  document.body.appendChild(page);
  return page;
}

/**
 * Read the text inside an <error-banner> host in the page's shadow root.
 * The banner renders its alert paragraph inside its own shadow root.
 */
function bannerText(page: AccountPage, id: string): string | undefined {
  const host = page.shadowRoot?.querySelector(`#${id}`);
  return host?.shadowRoot?.querySelector(".error")?.textContent?.trim();
}

function fill(page: AccountPage, id: string, value: string) {
  const input = page.shadowRoot?.querySelector(`#${id}`) as HTMLInputElement;
  input.value = value;
  input.dispatchEvent(new InputEvent("input"));
}

function submit(page: AccountPage) {
  const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
  form.dispatchEvent(new Event("submit", { cancelable: true }));
}

/** The rendered content cards, in document order (the favorites rows). */
function cards(page: AccountPage): ContentCard[] {
  return [
    ...(page.shadowRoot?.querySelectorAll("content-card") ?? []),
  ] as ContentCard[];
}

/** A card's own shadow root — the card renders its content there. */
function cardRoot(card: ContentCard): HTMLElement {
  return card.shadowRoot! as unknown as HTMLElement;
}

/** The notifications tab's full visible label — the catalog parts plus the
 * parenthetical punctuation, so a test never hand-types the enrichment. */
const NOTIFICATIONS_TAB = `${el.account.notificationsTab} (${el.account.notificationsTabPush})`;

/** Switch the account page's section tab by its (Greek) label — the tab
 * chrome uses internal-state buttons. The notifications tab's label carries
 * its parenthetical anglicism, hence the shared NOTIFICATIONS_TAB constant
 * above. */
async function switchTab(page: AccountPage, label: string): Promise<void> {
  const btn = [
    ...(page.shadowRoot?.querySelectorAll(".account-tab") ?? []),
  ].find((b) => b.textContent?.trim() === label) as HTMLElement | undefined;
  btn?.click();
  await page.updateComplete;
  await settle();
  await page.updateComplete;
}

/** Settle the microtask/update queue — the file's standard await pattern
 * after firing an async handler or letting a fetch resolve. */
async function settle(): Promise<void> {
  await new Promise((r) => setTimeout(r, 10));
}

/** The profile fixture shared by the authenticated-view tests. */
const PROFILE_BODY = {
  id: "u1",
  username: "Katakuri",
  email: "katakuri@example.com",
  role: "user",
  emailVerified: true,
  createdAt: "2023-11-14T22:13:20.000Z",
  avatarUrl: null,
};

/** The empty favorites envelope — the default for tests
 * that care only about the profile/form/sign-out parts. */
const EMPTY_FAVORITES = {
  items: [],
  pageInfo: { hasNextPage: false, endCursor: null, total: 0 },
};

/** The empty sessions envelope — the default for tests that
 * care only about the profile/form/sign-out parts. */
const EMPTY_SESSIONS = {
  items: [],
  pageInfo: { hasNextPage: false, endCursor: null, total: 0 },
};

/** One live session row. */
const SESSION_ITEM = {
  id: "s2",
  clientLabel: "Chrome · Android",
  createdAt: "2023-11-14T22:13:20.000Z",
  expiresAt: "2023-11-21T22:13:20.000Z",
  current: false,
};

/** The requesting session — labeled as the current one, so it renders the
 * badge instead of a revoke button. */
const CURRENT_SESSION_ITEM = {
  id: "s1",
  clientLabel: null,
  createdAt: "2023-11-13T10:00:00.000Z",
  expiresAt: "2023-11-20T10:00:00.000Z",
  current: true,
};

/** One favorites card. */
const FAV_ITEM = {
  id: "b1",
  title: "B1",
  subtitle: "Υπότιτλος B1",
  description: "blog one",
  thumbnailUrl: "https://fans.example/media/images/b1.jpg",
  publishedAt: "2023-11-14T22:13:20.000Z",
  favoritedAt: "2023-11-14T22:13:21.000Z",
  commentCount: 3,
  favoriteCount: 0,
};

/**
 * Route the sessions read for tests that override globalThis.fetch wholesale:
 * returns the canned list response for that URL, else null so the test's own
 * branches continue.
 */
function routeSessions(url: string, jsonBody: unknown): Response | null {
  return url.includes("/users/me/sessions")
    ? mockResponse({ ok: true, status: 200, jsonBody })
    : null;
}

/**
 * The default URL-routed fetch for authenticated renders: the profile GET
 * answers PROFILE_BODY, every favorites call answers the empty envelope.
 * Tests that need other behavior overwrite globalThis.fetch themselves.
 */
function defaultAccountFetch(): typeof fetch {
  return (async (input: RequestInfo | URL) => {
    const url =
      typeof input === "string"
        ? input
        : input instanceof URL
          ? input.href
          : input.url;
    if (url.includes("/users/me/favorites/")) {
      return mockResponse({ ok: true, status: 200, jsonBody: EMPTY_FAVORITES });
    }
    const sessions = routeSessions(url, EMPTY_SESSIONS);
    if (sessions) return sessions;
    if (url.includes("/users/me")) {
      return mockResponse({ ok: true, status: 200, jsonBody: PROFILE_BODY });
    }
    throw new TypeError(`unrouted fetch in test: ${url}`);
  }) as unknown as typeof fetch;
}

let originalLocation: Location;

/** Run-scoped window listeners/stubs — drained in afterEach (one happy-dom
 * window is shared per bun worker). */
const windowCleanups: Array<() => void> = [];

/** The revoke spy of the current test — restored in afterEach (a global
 * patch must never outlive its test, even on a failed assertion). */
let revokeSpy: ReturnType<typeof spyOn> | null = null;

/**
 * Mirror happy-dom's missing browser behavior for the favorites tab test:
 * pushState must also move window.location (verified by probe in the
 * search-page suite — happy-dom does not sync them; browsers do). The
 * account page's location is a stub object, so the shim writes its
 * `search` property directly.
 */
function shimTabHistorySync() {
  const push = window.history.pushState.bind(window.history);
  Object.defineProperty(window.history, "pushState", {
    configurable: true,
    value: (data: unknown, _unused: string, url?: string) => {
      push(data, _unused, url);
      if (url) {
        const search = url.includes("?") ? url.slice(url.indexOf("?")) : "";
        (window.location as { search: string }).search = search;
      }
    },
  });
}

beforeEach(() => {
  document.body.innerHTML = "";
  // Stub window.location so replaceState/pushState redirects are inert.
  originalLocation = window.location;
  Object.defineProperty(window, "location", {
    value: { href: "", search: "" },
    writable: true,
    configurable: true,
  });
  shimTabHistorySync();
  // The default fetch answers the profile + favorites reads so every
  // authenticated render behaves; tests override it for failure paths.
  mockFetchImpl(defaultAccountFetch());
});

afterEach(() => {
  for (const cleanup of windowCleanups) cleanup();
  windowCleanups.length = 0;
  document.body.innerHTML = "";
  // Restore the REAL location object — delete would remove it entirely
  // (it is an own property here) and break location-dependent tests in
  // later files (the search-page URL-sync tests).
  Object.defineProperty(window, "location", {
    value: originalLocation,
    writable: true,
    configurable: true,
  });
  // Undo the pushState patch (see shimTabHistorySync): an un-restored
  // wrapper would outlive this file, and its location.search write would
  // throw against the REAL location's getter-only search in later files.
  // delete reveals the prototype
  // method — no non-writable own property left behind.
  delete (window.history as unknown as { pushState?: unknown }).pushState;
  revokeSpy?.mockRestore();
  revokeSpy = null;
  restoreMockFetch();
  // The storage keys: a leaked banner would re-render in the next
  // test's fresh page (the one-shot read happens on mount).
  sessionStorage.removeItem("sf-verify-success");
  sessionStorage.removeItem("sf-register-success");
  // The revoke test sets a CSRF token; a leaked one would attach to the next
  // test's unsafe requests.
  clearCSRFToken();
});

// ── Rendering ──────────────────────────────────────────────────────

describe("account-page", () => {
  test("shows loading spinner when initializing", async () => {
    const page = render({ status: "initializing" });
    await page.updateComplete;

    const spinner = page.shadowRoot?.querySelector("loading-spinner");
    expect(spinner).not.toBeNull();
    const loading = spinner?.shadowRoot?.querySelector(".loading");
    expect(loading?.textContent).toContain(el.ui.loading);
    expect(loading?.getAttribute("role")).toBe("status");
    // The spinner branch keeps the route's heading landmark — the only h1
    // in the branch, so the page never renders headingless while it waits.
    const headings = [...(page.shadowRoot?.querySelectorAll("h1") ?? [])];
    expect(headings).toHaveLength(1);
    expect(headings[0]?.classList.contains("sr-only")).toBe(true);
    expect(headings[0]?.textContent?.trim()).toBe(el.ui.account);
  });

  test("the anonymous redirect spinner keeps the heading landmark", async () => {
    const page = render({ status: "anonymous" });
    await page.updateComplete;

    expect(page.shadowRoot?.querySelector("loading-spinner")).not.toBeNull();
    const headings = [...(page.shadowRoot?.querySelectorAll("h1") ?? [])];
    expect(headings).toHaveLength(1);
    expect(headings[0]?.classList.contains("sr-only")).toBe(true);
    expect(headings[0]?.textContent?.trim()).toBe(el.ui.account);
  });

  test("shows bootstrap error with retry button", async () => {
    const retrySpy = mock(() => {});
    const page = render({
      status: "error",
      message: "bootstrap boom",
      retryBootstrap: retrySpy,
    });
    await page.updateComplete;

    const error = page.shadowRoot?.querySelector("error-banner");
    expect(
      error?.shadowRoot?.querySelector(".error")?.textContent?.trim(),
    ).toBe("bootstrap boom");

    const retry = page.shadowRoot?.querySelector(
      ".retry-btn",
    ) as HTMLButtonElement;
    retry?.click();
    expect(retrySpy).toHaveBeenCalledTimes(1);
  });

  test("renders account view with catalog labels when authenticated", async () => {
    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    expect(page.shadowRoot?.querySelector("h1")?.textContent).toBe(
      el.ui.account,
    );

    // Profile facts (GET /api/v1/users/me — the fetch mock above).
    const dts = [
      ...(page.shadowRoot?.querySelectorAll(".profile-facts dt") ?? []),
    ].map((d) => d.textContent?.trim());
    expect(dts).toContain(el.ui.username);
    expect(dts).toContain(el.ui.email);
    expect(dts).toContain(el.account.memberSince);
    expect(dts).toContain(el.account.roleLabel);

    // The push settings + install control live on the NOTIFICATIONS tab
    // (the two are one story). The push settings component owns its own
    // surface.
    await switchTab(page, NOTIFICATIONS_TAB);
    expect(page.shadowRoot?.querySelector("push-settings")).not.toBeNull();

    // The install control sits WITH the push section: it stays a
    // sibling immediately BEFORE it. It stays dormant unless the browser
    // offers a prompt (or on iOS), so the pin is its presence AND its
    // placement.
    const control = page.shadowRoot?.querySelector("install-control");
    const pushSettings = page.shadowRoot?.querySelector("push-settings");
    expect(control).not.toBeNull();
    expect(control?.parentElement).toBe(pushSettings?.parentElement);
    expect(control?.nextElementSibling).toBe(pushSettings);

    // Password form fields live on the SECURITY tab — the page's sections
    // are tabs, so the form is not in the DOM on the default profile panel.
    await switchTab(page, el.account.securityTab);
    const labels = [...(page.shadowRoot?.querySelectorAll("label") ?? [])].map(
      (l) => l.textContent?.trim(),
    );
    expect(labels).toContain(el.ui.currentPassword);
    expect(labels).toContain(el.ui.newPassword);
    expect(labels).toContain(el.ui.confirmPassword);

    // Sign-out lives on the DEVICES panel, with the sessions it addresses —
    // and appears on no other tab.
    await switchTab(page, el.ui.sessions);
    expect(
      page.shadowRoot?.querySelector(".signout-btn")?.textContent?.trim(),
    ).toBe(el.ui.signOut);
    expect(
      page.shadowRoot?.querySelector(".signout-all-btn")?.textContent?.trim(),
    ).toBe(el.auth.signOutAll);
    expect(
      page.shadowRoot
        ?.querySelector(".signout-actions")
        ?.closest("#account-panel-devices"),
    ).not.toBeNull();
    await switchTab(page, el.account.profileTitle);
    expect(page.shadowRoot?.querySelector(".signout-actions")).toBeNull();
  });

  test("shows the forced-change notice when the session is flagged", async () => {
    const page = render({ mustChangePassword: true });
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    const notice = page.shadowRoot?.querySelector(".forced-change-notice");
    expect(notice).not.toBeNull();
    expect(notice?.textContent?.trim()).toBe(el.auth.forcedChangeNotice);
    expect(notice?.getAttribute("role")).toBe("alert");
  });

  test("hides the forced-change notice for a normal session", async () => {
    const page = render({ mustChangePassword: false });
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    expect(page.shadowRoot?.querySelector(".forced-change-notice")).toBeNull();
  });

  test("account sections are tabs: profile default, panels exclusive", async () => {
    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    // Default: the profile panel only — no favorites list, no password
    // form (the sections are exclusive tab panels).
    expect(page.shadowRoot?.querySelector(".profile")).not.toBeNull();
    expect(page.shadowRoot?.querySelector(".fav-tabs")).toBeNull();
    expect(page.shadowRoot?.querySelector("form")).toBeNull();

    // The tab chrome carries the five section labels.
    const tabs = [
      ...(page.shadowRoot?.querySelectorAll(".account-tab") ?? []),
    ].map((b) => b.textContent?.trim());
    expect(tabs).toEqual([
      el.account.profileTitle,
      el.favorites.sectionTitle,
      `${el.account.notificationsTab} (${el.account.notificationsTabPush})`,
      el.ui.sessions,
      el.account.securityTab,
    ]);

    // Only one panel mounts, so the tabs carry no aria-controls — an IDREF
    // to an unmounted panel would dangle (axe). The panel is labelled by
    // its tab instead.
    for (const tab of page.shadowRoot?.querySelectorAll(".account-tab") ?? []) {
      expect(tab.getAttribute("aria-controls")).toBeNull();
    }
    const activePanel = page.shadowRoot?.querySelector(
      "#account-panel-profile",
    );
    expect(activePanel?.getAttribute("role")).toBe("tabpanel");
    expect(activePanel?.getAttribute("aria-labelledby")).toBe(
      "account-tab-profile",
    );

    // The parenthetical is a deliberate anglicism — it renders inside
    // lang="en" so assistive tech pronounces it correctly.
    expect(
      page.shadowRoot
        ?.querySelector("#account-tab-notifications span")
        ?.getAttribute("lang"),
    ).toBe("en");

    await switchTab(page, el.account.securityTab);
    expect(page.shadowRoot?.querySelector("form")).not.toBeNull();
    expect(page.shadowRoot?.querySelector(".profile")).toBeNull();

    await switchTab(page, el.favorites.sectionTitle);
    expect(page.shadowRoot?.querySelector(".fav-tabs")).not.toBeNull();
    expect(page.shadowRoot?.querySelector("form")).toBeNull();

    // The notifications panel hosts the two notification surfaces (the
    // install control + the push settings) and nothing else.
    await switchTab(page, NOTIFICATIONS_TAB);
    expect(page.shadowRoot?.querySelector("push-settings")).not.toBeNull();
    expect(page.shadowRoot?.querySelector(".profile")).toBeNull();
    expect(page.shadowRoot?.querySelector(".fav-tabs")).toBeNull();

    // The devices panel hosts the session list.
    await switchTab(page, el.ui.sessions);
    expect(
      page.shadowRoot?.querySelector("#sessions-heading")?.textContent?.trim(),
    ).toBe(el.ui.sessions);
    expect(page.shadowRoot?.querySelector(".profile")).toBeNull();
  });

  test("the tablist follows the APG keyboard contract (roving tabindex, arrows, Home/End)", async () => {
    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    const tabs = [
      ...(page.shadowRoot?.querySelectorAll<HTMLButtonElement>(
        ".account-tab",
      ) ?? []),
    ];
    const order = () => tabs.map((t) => t.getAttribute("tabindex"));
    const activeId = () => page.shadowRoot?.activeElement?.id;

    // Roving tabindex: the tablist is ONE tab stop — the active tab — while
    // every tab stays focusable in the shadow tree.
    expect(order()).toEqual(["0", "-1", "-1", "-1", "-1"]);

    const press = async (key: string) => {
      const target = page.shadowRoot?.activeElement as HTMLElement;
      target.dispatchEvent(
        new KeyboardEvent("keydown", { key, bubbles: true }),
      );
      await page.updateComplete;
      await settle();
      await page.updateComplete;
    };

    tabs[0]!.focus();
    // Automatic activation: an arrow moves focus AND selection together,
    // and focus stays on the tab so arrows keep walking the tablist.
    await press("ArrowLeft");
    expect(activeId()).toBe("account-tab-security");
    expect(tabs[4]!.getAttribute("aria-selected")).toBe("true");
    expect(order()).toEqual(["-1", "-1", "-1", "-1", "0"]);
    expect(
      page.shadowRoot?.querySelector("#account-panel-security"),
    ).not.toBeNull();

    // ArrowRight wraps forwards from the last tab.
    await press("ArrowRight");
    expect(activeId()).toBe("account-tab-profile");
    expect(order()).toEqual(["0", "-1", "-1", "-1", "-1"]);
    expect(
      page.shadowRoot?.querySelector("#account-panel-profile"),
    ).not.toBeNull();

    // Home/End jump to the ends.
    await press("End");
    expect(activeId()).toBe("account-tab-security");
    await press("Home");
    expect(activeId()).toBe("account-tab-profile");
    expect(tabs[0]!.getAttribute("aria-selected")).toBe("true");
  });

  test("a direct activation hands focus to the activated panel", async () => {
    const page = render();
    await page.updateComplete;
    await settle();

    await switchTab(page, el.account.securityTab);

    // The panel is labelled by its tab, so the activation is announced
    // there; the panels keep tabindex="-1" for exactly this focus move.
    expect(page.shadowRoot?.activeElement?.id).toBe("account-panel-security");
  });

  // ── Profile section ───────────────────────────────────────────

  test("profile section shows a spinner while the profile loads", async () => {
    // A fetch that never settles keeps the pending state visible.
    globalThis.fetch = mock(
      () => new Promise(() => {}),
    ) as unknown as typeof fetch;
    const page = render();
    await page.updateComplete;

    const section = page.shadowRoot?.querySelector(".profile") ?? null;
    expect(section).toBeNull();
    expect(page.shadowRoot?.querySelector("loading-spinner")).not.toBeNull();
  });

  test("profile facts render from GET /api/v1/users/me", async () => {
    const requests: string[] = [];
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url =
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.href
            : input.url;
      requests.push(url);
      if (url.includes("/users/me/favorites/")) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: EMPTY_FAVORITES,
        });
      }
      return mockResponse({
        ok: true,
        status: 200,
        jsonBody: { ...PROFILE_BODY, role: "admin" },
      });
    }) as unknown as typeof fetch;

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    expect(requests).toContain("/api/v1/users/me");

    const dds = [
      ...(page.shadowRoot?.querySelectorAll(".profile-facts dd") ?? []),
    ].map((d) => d.textContent?.trim());
    expect(dds).toContain("Katakuri");
    // The email fact carries the address + the verified-status chip.
    expect(dds.some((v) => v?.includes("katakuri@example.com"))).toBe(true);
    expect(dds.some((v) => v?.includes(el.account.emailVerifiedLabel))).toBe(
      true,
    );
    // Role identifiers map to Greek catalog copy.
    expect(dds).toContain(el.account.roleAdmin);
    // Member-since is the formatted creation instant (locale rendering is
    // environment-dependent — assert presence, not the exact string).
    expect(dds.some((v) => v && v.length > 0)).toBe(true);

    // The initials-circle placeholder shows the username's first letter.
    expect(page.shadowRoot?.querySelector(".avatar")?.textContent?.trim()).toBe(
      "K",
    );
  });

  test("profile failure shows the banner and retry refetches", async () => {
    let profileCalls = 0;
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url =
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.href
            : input.url;
      const sessions = routeSessions(url, EMPTY_SESSIONS);
      if (sessions) return sessions;
      if (url.includes("/users/me/favorites/")) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: EMPTY_FAVORITES,
        });
      }
      profileCalls++;
      if (profileCalls === 1) {
        return mockResponse({
          ok: false,
          status: 500,
          headers: { "Content-Type": "application/problem+json" },
          jsonBody: {
            type: "/problems/internal-error",
            title: "Internal server error",
            status: 500,
          },
        });
      }
      return mockResponse({ ok: true, status: 200, jsonBody: PROFILE_BODY });
    }) as unknown as typeof fetch;

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    // First profile load failed — Greek problem copy in the banner, no facts.
    expect(profileCalls).toBe(1);
    expect(
      page.shadowRoot
        ?.querySelector("error-banner")
        ?.shadowRoot?.querySelector(".error")
        ?.textContent?.trim(),
    ).toBe(el.problems["/problems/internal-error"]);
    expect(page.shadowRoot?.querySelector(".profile")).toBeNull();

    // Retry re-fetches and renders the facts.
    const retry = page.shadowRoot?.querySelector(
      ".retry-btn",
    ) as HTMLButtonElement;
    retry?.click();
    await settle();
    await page.updateComplete;

    expect(profileCalls).toBe(2);
    expect(page.shadowRoot?.querySelector(".profile")).not.toBeNull();
    expect(page.shadowRoot?.querySelector("error-banner")).toBeNull();
  });

  test("profile is not fetched while anonymous", async () => {
    const fetchSpy = mock(async () =>
      mockResponse({
        ok: true,
        status: 200,
        jsonBody: {},
      }),
    );
    globalThis.fetch = fetchSpy as unknown as typeof fetch;

    const nav = listenForNavigation();
    try {
      const page = render({ status: "anonymous" });
      await page.updateComplete;
      await settle();

      expect(fetchSpy).not.toHaveBeenCalled();
      expect(nav.path).toBe("/sign-in");
    } finally {
      nav.remove();
    }
  });

  // ── Favorites section ───────────────────────────────────────────

  test("favorites tabs render with posts active and the empty state", async () => {
    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    // The favorites section is a tab panel; the load is gated on the
    // active tab.
    await switchTab(page, el.favorites.sectionTitle);

    const radios = [
      ...(page.shadowRoot?.querySelectorAll('.fav-tab input[type="radio"]') ??
        []),
    ] as HTMLInputElement[];
    expect(radios).toHaveLength(2);
    expect(radios[0]?.checked).toBe(true); // posts is the default tab
    expect(radios[1]?.checked).toBe(false);

    const tabs = [
      ...(page.shadowRoot?.querySelectorAll(".fav-tab span") ?? []),
    ].map((s) => s.textContent?.trim());
    expect(tabs).toContain(el.favorites.tabPosts);
    expect(tabs).toContain(el.favorites.tabProjects);

    // The default fetch answers with an empty envelope.
    const empty = page.shadowRoot?.querySelector(".fav-empty");
    expect(empty?.textContent?.trim()).toBe(el.favorites.emptyPosts);
    expect(page.shadowRoot?.querySelector("pager-nav")).toBeNull();
    // The reserved-footprint wrapper — the account tab
    // row above must not resize between the favorites states.
    expect(empty?.closest(".state-block")).not.toBeNull();
  });

  test("a ?tab= deep link opens on favorites", async () => {
    (window.location as { search: string }).search = "?tab=favorites";
    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    expect(
      page.shadowRoot?.querySelector("#account-panel-favorites"),
    ).not.toBeNull();
    expect(page.shadowRoot?.querySelector("#account-panel-profile")).toBeNull();
  });

  test("the in-flight favorites load dedupes the double sync to one request", async () => {
    const calls: string[] = [];
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : (input as Request).url;
      if (url.includes("/users/me/favorites/")) {
        calls.push(url);
        // Never settles: the mount-time paging sync and the auth latch must
        // coalesce on the in-flight key instead of aborting and refetching.
        return new Promise<Response>(() => {});
      }
      return mockResponse({ ok: true, status: 200, jsonBody: PROFILE_BODY });
    }) as unknown as typeof fetch;
    (window.location as { search: string }).search = "?tab=favorites";

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    expect(calls.length).toBe(1);
  });

  test("a favorites page turn starts at the top", async () => {
    const scrollCalls: Array<[number, number]> = [];
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url =
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.href
            : input.url;
      if (url.includes("/users/me/favorites/")) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: url.includes("after=")
            ? {
                items: [FAV_ITEM],
                pageInfo: { hasNextPage: false, endCursor: null, total: 2 },
              }
            : {
                items: [FAV_ITEM],
                pageInfo: { hasNextPage: true, endCursor: "cur1", total: 2 },
              },
        });
      }
      return mockResponse({ ok: true, status: 200, jsonBody: PROFILE_BODY });
    }) as unknown as typeof fetch;

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;
    await switchTab(page, el.favorites.sectionTitle);

    // The page turn builds its URL against the stubbed location (the suite
    // replaces window.location with a plain object).
    Object.assign(window.location as unknown as Record<string, string>, {
      href: "https://example.com/account?tab=posts",
      origin: "https://example.com",
    });
    windowCleanups.push(
      stubProperty(window, "scrollTo", (x: number, y: number) => {
        scrollCalls.push([x, y]);
      }),
    );
    const pager = page.shadowRoot?.querySelector("pager-nav") as HTMLElement;
    pager.dispatchEvent(new CustomEvent("sf-pager-next"));
    await settle();
    await page.updateComplete;

    // The turn loaded the cursor page and reset the scroll; a popstate-synced
    // load keeps the browser's restored position (no call).
    expect(scrollCalls).toEqual([[0, 0]]);
  });

  test("the first authenticated favorites load keeps the restored scroll position", async () => {
    const scrollCalls: Array<[number, number]> = [];
    windowCleanups.push(
      stubProperty(window, "scrollTo", (x: number, y: number) => {
        scrollCalls.push([x, y]);
      }),
    );
    (window.location as { search: string }).search = "?tab=favorites";

    // A cold load: the session resolves after connect, so the FIRST load
    // arrives through the auth latch — it must not fight the browser's
    // scroll restoration (the page-turn test above pins the other side).
    const page = render({ status: "initializing" });
    await page.updateComplete;
    await settle();
    await page.updateComplete;
    (page as any).session.state.status = "authenticated";
    page.requestUpdate();
    await waitFor(() => page.shadowRoot?.querySelector(".fav-empty") !== null);

    expect(scrollCalls).toEqual([]);
  });

  test("favorites list shows a spinner while loading", async () => {
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url =
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.href
            : input.url;
      if (url.includes("/users/me/favorites/")) {
        // The favorites load never settles — the spinner stays visible.
        return new Promise(() => {});
      }
      return mockResponse({ ok: true, status: 200, jsonBody: PROFILE_BODY });
    }) as unknown as typeof fetch;

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    // Activate the favorites tab — its load never settles (see the mock),
    // so the spinner must stay visible on the panel.
    await switchTab(page, el.favorites.sectionTitle);

    // The favorites panel still shows its spinner and no list/empty state.
    expect(
      page.shadowRoot?.querySelectorAll("loading-spinner").length,
    ).toBeGreaterThan(0);
    expect(page.shadowRoot?.querySelector(".fav-results")).toBeNull();
    expect(page.shadowRoot?.querySelector(".fav-empty")).toBeNull();
  });

  test("favorites list renders dense row cards linking to details", async () => {
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url =
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.href
            : input.url;
      if (url.includes("/users/me/favorites/blog-posts")) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: {
            items: [FAV_ITEM],
            pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
          },
        });
      }
      return mockResponse({ ok: true, status: 200, jsonBody: PROFILE_BODY });
    }) as unknown as typeof fetch;

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    await switchTab(page, el.favorites.sectionTitle);

    const card = cards(page)[0] as ContentCard;
    // The card's inputs: the dense row shape, the detail link, and the
    // content fields the row reads.
    expect(card.getAttribute("shape")).toBe("row");
    expect(card.hasAttribute("dense")).toBe(true);
    expect(card.getAttribute("href")).toBe("/blog/b1");
    expect(card.getAttribute("title")).toBe("B1");
    expect(card.getAttribute("subtitle")).toBe(FAV_ITEM.subtitle);
    expect(card.getAttribute("heading-level")).toBe("3");
    expect(card.getAttribute("thumbnail-url")).toBe(FAV_ITEM.thumbnailUrl);
    expect(card.getAttribute("meta-instant")).toBe(FAV_ITEM.publishedAt);
    expect(card.commentCount).toBe(FAV_ITEM.commentCount);
    expect(card.favoriteCount).toBe(FAV_ITEM.favoriteCount);

    // What the row renders from them: the title as a heading, the plain
    // description, the date, and the detail link on the frame.
    const shadow = cardRoot(card);
    expect(shadow.querySelector("h3.title")?.textContent?.trim()).toBe("B1");
    expect(shadow.querySelector(".subtitle")?.textContent?.trim()).toBe(
      FAV_ITEM.subtitle,
    );
    expect(shadow.querySelector(".subtitle-sep")).not.toBeNull();
    expect(shadow.querySelector(".description")?.textContent?.trim()).toBe(
      FAV_ITEM.description,
    );
    expect(shadow.querySelector("time")?.getAttribute("datetime")).toBe(
      FAV_ITEM.publishedAt,
    );
    expect(shadow.querySelector("a.frame")?.getAttribute("href")).toBe(
      "/blog/b1",
    );

    // The indicator chip renders on the favorite card — both
    // indicators always, zeros included.
    const stats = shadow.querySelector(".content-stats");
    expect(stats?.querySelectorAll(".stat").length).toBe(2);
    expect(stats?.querySelector('[aria-label="0 αγαπημένα"]')).not.toBeNull();

    // The remove control is an ICON button: the glyph is the
    // visual, and the accessible name AND the hover title carry the same
    // catalog string — the convention every converted control follows. It
    // rides the card's overlay slot, so the card owns the corner placement.
    const remove = card.querySelector(".fav-remove");
    expect(remove?.textContent?.trim()).toBe("");
    expect(remove?.getAttribute("aria-label")).toBe(el.favorites.removeAria);
    expect(remove?.getAttribute("title")).toBe(el.favorites.removeAria);
    expect(remove?.querySelector("svg")).not.toBeNull();
    expect(remove?.getAttribute("slot")).toBe("overlay");
  });

  test("switching to the projects tab loads project favorites", async () => {
    const favURLs: string[] = [];
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url =
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.href
            : input.url;
      if (url.includes("/users/me/favorites/")) {
        favURLs.push(url);
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody:
            favURLs.length === 1
              ? EMPTY_FAVORITES
              : {
                  items: [{ ...FAV_ITEM, id: "p1", title: "P1", subtitle: "" }],
                  pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
                },
        });
      }
      return mockResponse({ ok: true, status: 200, jsonBody: PROFILE_BODY });
    }) as unknown as typeof fetch;

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    await switchTab(page, el.favorites.sectionTitle);

    const projectsRadio = page.shadowRoot?.querySelector(
      '.fav-tab input[value="projects"]',
    ) as HTMLInputElement;
    projectsRadio?.click();
    await settle();
    await page.updateComplete;

    expect(favURLs[1]).toContain("/users/me/favorites/projects");
    // The projects label is a whole-value anglicism scoped lang="en"; the
    // Greek posts label renders plain.
    const anglicism = page.shadowRoot?.querySelector(
      '.fav-tab span[lang="en"]',
    );
    expect(anglicism?.textContent?.trim()).toBe(el.favorites.tabProjects);
    expect(
      page.shadowRoot?.querySelector(
        '.fav-tab input[value="posts"] ~ span[lang="en"]',
      ),
    ).toBeNull();
    expect(cards(page)[0]?.getAttribute("href")).toBe("/projects/p1");
    // A project favorite carries no subtitle: the row keeps its plain title.
    expect(
      cardRoot(cards(page)[0] as ContentCard).querySelector(".title-line"),
    ).toBeNull();
  });

  test("remove button unfavorites and reloads to the empty state", async () => {
    let listCalls = 0;
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url =
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.href
            : input.url;
      if (url.includes("/users/me/favorites/blog-posts/b1")) {
        return mockResponse({ ok: true, status: 204, jsonBody: "" });
      }
      if (url.includes("/users/me/favorites/")) {
        listCalls++;
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody:
            listCalls === 1
              ? {
                  items: [FAV_ITEM],
                  pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
                }
              : EMPTY_FAVORITES,
        });
      }
      return mockResponse({ ok: true, status: 200, jsonBody: PROFILE_BODY });
    }) as unknown as typeof fetch;

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    await switchTab(page, el.favorites.sectionTitle);

    const remove = page.shadowRoot?.querySelector(
      ".fav-remove",
    ) as HTMLButtonElement;
    remove?.click();
    await settle();
    await page.updateComplete;

    // The reload after removal lands on the empty state.
    expect(listCalls).toBe(2);
    expect(
      page.shadowRoot?.querySelector(".fav-empty")?.textContent?.trim(),
    ).toBe(el.favorites.emptyPosts);
  });

  test("favorites load failure shows the banner and retry reloads", async () => {
    let listCalls = 0;
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url =
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.href
            : input.url;
      if (url.includes("/users/me/favorites/")) {
        listCalls++;
        if (listCalls === 1) {
          return mockResponse({
            ok: false,
            status: 500,
            headers: { "Content-Type": "application/problem+json" },
            jsonBody: {
              type: "/problems/internal-error",
              title: "Internal server error",
              status: 500,
            },
          });
        }
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: EMPTY_FAVORITES,
        });
      }
      return mockResponse({ ok: true, status: 200, jsonBody: PROFILE_BODY });
    }) as unknown as typeof fetch;

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    await switchTab(page, el.favorites.sectionTitle);

    // The section-level banner (the load failed on the favorites panel;
    // the profile panel is a separate tab now).
    expect(
      page.shadowRoot
        ?.querySelector("error-banner")
        ?.shadowRoot?.querySelector(".error")
        ?.textContent?.trim(),
    ).toBe(el.problems["/problems/internal-error"]);

    const retry = page.shadowRoot?.querySelector(
      ".retry-btn",
    ) as HTMLButtonElement;
    retry?.click();
    await settle();
    await page.updateComplete;

    expect(listCalls).toBe(2);
    expect(page.shadowRoot?.querySelector("error-banner")).toBeNull();
    expect(
      page.shadowRoot?.querySelector(".fav-empty")?.textContent?.trim(),
    ).toBe(el.favorites.emptyPosts);
  });

  test("429 disables the password submit for the Retry-After window only", async () => {
    const changeSpy = mock(async () => {
      throw new ApiError(
        {
          type: "/problems/rate-limited",
          title: "Too many requests",
          status: 429,
        },
        1,
      );
    });

    const page = render({ changePassword: changeSpy });
    await page.updateComplete;

    // The password form is on the SECURITY tab.
    await switchTab(page, el.account.securityTab);

    fill(page, "account-current-password", "old-secret");
    fill(page, "account-new-password", "new-secret-1");
    fill(page, "account-confirm-new-password", "new-secret-1");
    submit(page);
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    let submitBtn = page.shadowRoot?.querySelector(
      'button[type="submit"]',
    ) as HTMLButtonElement;

    // Only the rate-limited password form cools down.
    expect(submitBtn.disabled).toBe(true);
    expect(submitBtn.textContent?.trim()).toBe(
      "Δοκιμάστε ξανά σε 1 δευτερόλεπτο.",
    );

    // Sign-out actions are not rate-limited and must stay usable — they
    // live on the DEVICES tab.
    await switchTab(page, el.ui.sessions);
    expect(
      (page.shadowRoot?.querySelector(".signout-btn") as HTMLButtonElement)
        .disabled,
    ).toBe(false);
    expect(
      (page.shadowRoot?.querySelector(".signout-all-btn") as HTMLButtonElement)
        .disabled,
    ).toBe(false);

    // Back to the form for the implicit-submission path: a second submit
    // event during the cooldown must not call the API again.
    await switchTab(page, el.account.securityTab);
    submit(page);
    await page.updateComplete;
    expect(changeSpy).toHaveBeenCalledTimes(1);

    // Window closes → password submit re-enables with the normal label.
    await new Promise((r) => setTimeout(r, 1200));
    await page.updateComplete;
    submitBtn = page.shadowRoot?.querySelector(
      'button[type="submit"]',
    ) as HTMLButtonElement;
    expect(submitBtn.disabled).toBe(false);
    expect(submitBtn.textContent?.trim()).toBe(el.auth.changePassword);
  });

  test("redirects anonymous visitors to /sign-in", async () => {
    const nav = listenForNavigation();
    try {
      const page = render({ status: "anonymous" });
      await page.updateComplete;
      expect(nav.path).toBe("/sign-in");
    } finally {
      nav.remove();
    }
  });

  // ── Sign-out ────────────────────────────────────────────────────

  test("sign-out calls session.signOut", async () => {
    const nav = listenForNavigation(windowCleanups);
    const signOutSpy = mock(async () => {});
    const page = render({ signOut: signOutSpy });
    await page.updateComplete;

    // Sign-out lives on the DEVICES panel.
    await switchTab(page, el.ui.sessions);

    const btn = page.shadowRoot?.querySelector(
      ".signout-btn",
    ) as HTMLButtonElement;
    btn?.click();
    await new Promise((r) => setTimeout(r, 10));

    expect(signOutSpy).toHaveBeenCalledTimes(1);
    // The provider owns the state transition; until it arrives the page
    // stays put without an error banner (the observable post-condition).
    expect(nav.path).toBeNull();
    expect(page.shadowRoot?.querySelector("#session-action-error")).toBeNull();
  });

  test("sign-out-all calls session.signOutAll", async () => {
    const nav = listenForNavigation(windowCleanups);
    const signOutAllSpy = mock(async () => {});
    const page = render({ signOutAll: signOutAllSpy });
    await page.updateComplete;

    await switchTab(page, el.ui.sessions);

    const btn = page.shadowRoot?.querySelector(
      ".signout-all-btn",
    ) as HTMLButtonElement;
    btn?.click();
    await new Promise((r) => setTimeout(r, 10));

    expect(signOutAllSpy).toHaveBeenCalledTimes(1);
    expect(nav.path).toBeNull();
    expect(page.shadowRoot?.querySelector("#session-action-error")).toBeNull();
  });

  test("sign-out failure shows the problem banner and stays on the page", async () => {
    const signOutSpy = mock(async () => {
      throw new ApiError({
        type: "/problems/internal-error",
        title: "Internal server error",
        status: 500,
      });
    });
    const nav = listenForNavigation();
    try {
      const page = render({ signOut: signOutSpy });
      await page.updateComplete;
      await switchTab(page, el.ui.sessions);

      const btn = page.shadowRoot?.querySelector(
        ".signout-btn",
      ) as HTMLButtonElement;
      btn?.click();
      await new Promise((r) => setTimeout(r, 10));
      await page.updateComplete;

      // The session never flipped, so the guard must NOT redirect — the
      // failure banner shows instead and the account view stays put.
      expect(nav.path).toBeNull();
      expect(bannerText(page, "account-error")).toBe(
        el.problems["/problems/internal-error"],
      );
    } finally {
      nav.remove();
    }
  });

  test("sign-out-all network failure shows the generic banner and stays on the page", async () => {
    const signOutAllSpy = mock(async () => {
      throw new NetworkError("Offline");
    });
    const nav = listenForNavigation();
    try {
      const page = render({ signOutAll: signOutAllSpy });
      await page.updateComplete;
      await switchTab(page, el.ui.sessions);

      const btn = page.shadowRoot?.querySelector(
        ".signout-all-btn",
      ) as HTMLButtonElement;
      btn?.click();
      await new Promise((r) => setTimeout(r, 10));
      await page.updateComplete;

      expect(nav.path).toBeNull();
      expect(bannerText(page, "account-error")).toBe(el.ui.error);
    } finally {
      nav.remove();
    }
  });

  test("sign-out 403 revalidates to heal the stale CSRF token", async () => {
    const signOutSpy = mock(async () => {
      throw new ApiError({
        type: "/problems/forbidden",
        title: "Forbidden",
        status: 403,
      });
    });
    const revalidateSpy = mock(async () => {});
    const nav = listenForNavigation();
    try {
      const page = render({ signOut: signOutSpy, revalidate: revalidateSpy });
      await page.updateComplete;
      await switchTab(page, el.ui.sessions);

      const btn = page.shadowRoot?.querySelector(
        ".signout-btn",
      ) as HTMLButtonElement;
      btn?.click();
      await new Promise((r) => setTimeout(r, 10));
      await page.updateComplete;

      // The in-memory CSRF token is stale (cross-tab re-sign-in) — a
      // silent revalidate re-syncs it so a retry works.
      expect(revalidateSpy).toHaveBeenCalledTimes(1);
      expect(nav.path).toBeNull();
      expect(bannerText(page, "account-error")).toBe(
        el.problems["/problems/forbidden"],
      );
    } finally {
      nav.remove();
    }
  });

  test("sign-out-all 403 revalidates to heal the stale CSRF token", async () => {
    const signOutAllSpy = mock(async () => {
      throw new ApiError({
        type: "/problems/forbidden",
        title: "Forbidden",
        status: 403,
      });
    });
    const revalidateSpy = mock(async () => {});
    const page = render({
      signOutAll: signOutAllSpy,
      revalidate: revalidateSpy,
    });
    await page.updateComplete;
    await switchTab(page, el.ui.sessions);

    const btn = page.shadowRoot?.querySelector(
      ".signout-all-btn",
    ) as HTMLButtonElement;
    btn?.click();
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    expect(revalidateSpy).toHaveBeenCalledTimes(1);
  });

  // ── Password change ────────────────────────────────────────────

  test("successful password change shows the success view without redirect", async () => {
    const changeSpy = mock(async (_cur: string, _next: string) => {});
    const nav = listenForNavigation();
    try {
      const page = render({ changePassword: changeSpy });
      await page.updateComplete;

      await switchTab(page, el.account.securityTab);

      await fill(page, "account-current-password", "old-secret");
      await fill(page, "account-new-password", "new-secret-1");
      await fill(page, "account-confirm-new-password", "new-secret-1");
      await submit(page);
      await new Promise((r) => setTimeout(r, 10));
      await page.updateComplete;

      expect(changeSpy).toHaveBeenCalledWith("old-secret", "new-secret-1");
      expect(nav.path).toBeNull();
      const success = page.shadowRoot?.querySelector(".success");
      expect(success?.textContent?.trim()).toBe(el.auth.passwordChanged);
      const link = page.shadowRoot?.querySelector(".footer-link a");
      expect(link?.getAttribute("href")).toBe("/sign-in");
    } finally {
      nav.remove();
    }
  });

  test("wrong current password shows the field-level message", async () => {
    const changeSpy = mock(async () => {
      throw new ApiError({
        type: "/problems/auth/invalid-credentials",
        title: "Invalid credentials",
        status: 401,
      });
    });
    const revalidateSpy = mock(() => {});
    const page = render({
      changePassword: changeSpy,
      revalidate: revalidateSpy,
    });
    await page.updateComplete;

    await switchTab(page, el.account.securityTab);

    await fill(page, "account-current-password", "wrong");
    await fill(page, "account-new-password", "new-secret-1");
    await fill(page, "account-confirm-new-password", "new-secret-1");
    await submit(page);
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    const error = page.shadowRoot?.querySelector(
      "#account-current-password-error",
    );
    expect(error?.textContent?.trim()).toBe(el.auth.wrongCurrentPassword);
    // The ambiguous 401 must trigger the silent reconcile.
    expect(revalidateSpy).toHaveBeenCalledTimes(1);
  });

  test("change-password 403 revalidates to heal the stale CSRF token", async () => {
    const changeSpy = mock(async () => {
      throw new ApiError({
        type: "/problems/forbidden",
        title: "Forbidden",
        status: 403,
      });
    });
    const revalidateSpy = mock(() => {});
    const page = render({
      changePassword: changeSpy,
      revalidate: revalidateSpy,
    });
    await page.updateComplete;

    await switchTab(page, el.account.securityTab);

    // fill/submit are sync helpers — no await (void).
    fill(page, "account-current-password", "old-secret");
    fill(page, "account-new-password", "new-secret-1");
    fill(page, "account-confirm-new-password", "new-secret-1");
    submit(page);
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    // Same CSRF ambiguity as sign-out: a stale in-memory token 403s the
    // request — the silent revalidate re-syncs session + token.
    expect(revalidateSpy).toHaveBeenCalledTimes(1);
    expect(bannerText(page, "account-error")).toBe(
      el.problems["/problems/forbidden"],
    );
  });

  test("maps 422 violations to per-field errors", async () => {
    const changeSpy = mock(async () => {
      throw new ValidationError({
        type: "/problems/validation",
        title: "Validation failed",
        status: 422,
        violations: [
          {
            field: "newPassword",
            code: "minLength",
          },
        ],
      });
    });
    const revalidateSpy = mock(() => {});
    const page = render({
      changePassword: changeSpy,
      revalidate: revalidateSpy,
    });
    await page.updateComplete;

    await switchTab(page, el.account.securityTab);

    await fill(page, "account-current-password", "old-secret");
    await fill(page, "account-new-password", "short");
    await fill(page, "account-confirm-new-password", "short");
    await submit(page);
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    const error = page.shadowRoot?.querySelector("#account-new-password-error");
    expect(error?.textContent?.trim()).toBe(el.violations.minLength);
    // 422 implies a live, valid session — no reconcile round-trip.
    expect(revalidateSpy).not.toHaveBeenCalled();
  });

  test("mismatched confirmation blocks submit client-side", async () => {
    const changeSpy = mock(async (_cur: string, _next: string) => {});
    const page = render({ changePassword: changeSpy });
    await page.updateComplete;

    await switchTab(page, el.account.securityTab);

    await fill(page, "account-current-password", "old-secret");
    await fill(page, "account-new-password", "new-secret-1");
    await fill(page, "account-confirm-new-password", "different");
    await submit(page);
    await page.updateComplete;

    expect(changeSpy).not.toHaveBeenCalled();
    const error = page.shadowRoot?.querySelector(
      "#account-confirm-new-password-error",
    );
    expect(error?.textContent?.trim()).toBe(el.auth.passwordsDoNotMatch);
  });

  test("shows generic banner on network failure", async () => {
    const changeSpy = mock(async () => {
      throw new NetworkError("Offline");
    });
    const revalidateSpy = mock(() => {});
    const page = render({
      changePassword: changeSpy,
      revalidate: revalidateSpy,
    });
    await page.updateComplete;

    await switchTab(page, el.account.securityTab);

    await fill(page, "account-current-password", "old-secret");
    await fill(page, "account-new-password", "new-secret-1");
    await fill(page, "account-confirm-new-password", "new-secret-1");
    await submit(page);
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    expect(bannerText(page, "account-error")).toBe(el.ui.error);
    // Network failure after a possible server commit — reconcile silently.
    expect(revalidateSpy).toHaveBeenCalledTimes(1);
  });

  test("a request-deadline timeout also reconciles silently", async () => {
    const changeSpy = mock(async () => {
      throw new RequestTimeoutError();
    });
    const revalidateSpy = mock(() => {});
    const page = render({
      changePassword: changeSpy,
      revalidate: revalidateSpy,
    });
    await page.updateComplete;

    await switchTab(page, el.account.securityTab);

    await fill(page, "account-current-password", "old-secret");
    await fill(page, "account-new-password", "new-secret-1");
    await fill(page, "account-confirm-new-password", "new-secret-1");
    await submit(page);
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    expect(bannerText(page, "account-error")).toBe(el.ui.error);
    // The deadline may also land after a server commit — reconcile silently.
    expect(revalidateSpy).toHaveBeenCalledTimes(1);
  });

  // ── Focus management ───────────────────────────────────────────

  test("client-side errors focus the first invalid field", async () => {
    const page = render();
    await page.updateComplete;

    await switchTab(page, el.account.securityTab);

    await submit(page);
    await page.updateComplete;

    expect(page.shadowRoot?.activeElement?.id).toBe("account-current-password");
  });

  test("wrong-password error focuses the field after inputs re-enable", async () => {
    const changeSpy = mock(async () => {
      throw new ApiError({
        type: "/problems/auth/invalid-credentials",
        title: "Invalid credentials",
        status: 401,
      });
    });
    const page = render({ changePassword: changeSpy });
    await page.updateComplete;

    await switchTab(page, el.account.securityTab);

    await fill(page, "account-current-password", "wrong");
    await fill(page, "account-new-password", "new-secret-1");
    await fill(page, "account-confirm-new-password", "new-secret-1");
    await submit(page);
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    // Focus lands AFTER _pending flips false (a disabled input cannot
    // receive focus) — this is the pending-disable ordering pin.
    expect(page.shadowRoot?.activeElement?.id).toBe("account-current-password");
  });

  // ── Real provider tree (state-flip interleavings) ───────────────

  function routingFetch(
    routes: Record<
      string,
      (method: string) => Promise<{ status: number; body: unknown }>
    >,
  ) {
    globalThis.fetch = (async (
      input: RequestInfo | URL,
      init?: RequestInit,
    ) => {
      const url = typeof input === "string" ? input : "";
      const method = init?.method ?? "GET";
      for (const [pattern, handler] of Object.entries(routes)) {
        if (url.includes(pattern)) {
          const result = await handler(method);
          return {
            ok: result.status >= 200 && result.status < 300,
            status: result.status,
            headers: new Headers({ "content-type": "application/json" }),
            json: async () => result.body,
            text: async () => JSON.stringify(result.body),
          } as unknown as Response;
        }
      }
      throw new Error(`unexpected fetch: ${method} ${url}`);
    }) as unknown as typeof fetch;
  }

  async function mountWithProvider(page: AccountPage) {
    const provider = document.createElement(
      "session-provider",
    ) as SessionProvider;
    provider.appendChild(page);
    document.body.appendChild(provider);
    await provider.updateComplete;
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;
  }

  test("password change through the provider shows success without redirect", async () => {
    routingFetch({
      "/auth/session": () =>
        Promise.resolve({
          status: 200,
          body: {
            authenticated: true,
            csrfToken: "tok",
            user: { id: "u1", username: "Katakuri", role: "user" },
          },
        }),
      "/auth/password": (method) =>
        method === "PUT"
          ? Promise.resolve({ status: 200, body: { authenticated: false } })
          : Promise.resolve({ status: 500, body: {} }),
    });

    const nav = listenForNavigation();
    try {
      const page = document.createElement("account-page") as AccountPage;
      await mountWithProvider(page);

      await switchTab(page, el.account.securityTab);

      await fill(page, "account-current-password", "old-secret");
      await fill(page, "account-new-password", "new-secret-1");
      await fill(page, "account-confirm-new-password", "new-secret-1");
      await submit(page);
      await new Promise((r) => setTimeout(r, 10));
      await page.updateComplete;
      await new Promise((r) => setTimeout(r, 10));
      await page.updateComplete;

      // The provider flipped to anonymous AND _passwordChanged kept the
      // guard from redirecting — the exact interleaving this pins.
      expect(page.shadowRoot?.querySelector(".success")).not.toBeNull();
      expect(nav.path).toBeNull();
    } finally {
      nav.remove();
    }
  });

  test("sign-out through the provider redirects to /sign-in", async () => {
    routingFetch({
      "/auth/session": () =>
        Promise.resolve({
          status: 200,
          body: {
            authenticated: true,
            csrfToken: "tok",
            user: { id: "u1", username: "Katakuri", role: "user" },
          },
        }),
      "/auth/sign-out": (method) =>
        method === "POST"
          ? Promise.resolve({ status: 200, body: { authenticated: false } })
          : Promise.resolve({ status: 500, body: {} }),
    });

    const nav = listenForNavigation();
    try {
      const page = document.createElement("account-page") as AccountPage;
      await mountWithProvider(page);

      // Sign-out lives on the DEVICES panel.
      await switchTab(page, el.ui.sessions);

      const btn = page.shadowRoot?.querySelector(
        ".signout-btn",
      ) as HTMLButtonElement;
      btn?.click();
      await new Promise((r) => setTimeout(r, 10));
      await page.updateComplete;
      await new Promise((r) => setTimeout(r, 10));
      await page.updateComplete;

      expect(nav.path).toBe("/sign-in");
    } finally {
      nav.remove();
    }
  });

  // ── Avatar widget ───────────────────────────────────────────────

  test("the profile avatar renders the served image when set", async () => {
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url =
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.href
            : input.url;
      if (url.includes("/users/me/favorites/")) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: EMPTY_FAVORITES,
        });
      }
      const sessions = routeSessions(url, EMPTY_SESSIONS);
      if (sessions) return sessions;
      if (url.includes("/users/me")) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: {
            ...PROFILE_BODY,
            avatarUrl:
              "http://localhost:3000/media/images/ab1234567890abcdef1234567890abcd.png",
          },
        });
      }
      throw new TypeError(`unrouted fetch in test: ${url}`);
    }) as unknown as typeof fetch;

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    const img = page.shadowRoot?.querySelector("img.profile-avatar");
    expect(img?.getAttribute("src")).toBe(
      "http://localhost:3000/media/images/ab1234567890abcdef1234567890abcd.png",
    );
    expect(page.shadowRoot?.querySelector(".avatar-initial")).toBeNull();
    page.remove();
  });

  test("the profile shows the initials circle when avatarUrl is null", async () => {
    globalThis.fetch = defaultAccountFetch();

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    expect(page.shadowRoot?.querySelector("img.profile-avatar")).toBeNull();
    expect(
      page.shadowRoot?.querySelector(".avatar-initial")?.textContent?.trim(),
    ).toBe("K");
    page.remove();
  });

  test("choosing a file shows the preview and the upload button", async () => {
    globalThis.fetch = defaultAccountFetch();

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    // The object URL is local — assert the preview swapped to an image and
    // the confirm button appeared (the exact blob URL is browser-owned).
    const input = page.shadowRoot?.querySelector(
      "input[type=file]",
    ) as HTMLInputElement;
    const file = new File(["x"], "me.png", { type: "image/png" });
    Object.defineProperty(input, "files", {
      value: [file],
      configurable: true,
    });
    input.dispatchEvent(new Event("change"));
    await page.updateComplete;

    const img = page.shadowRoot?.querySelector("img.profile-avatar");
    expect(img?.getAttribute("src")?.startsWith("blob:")).toBe(true);
    expect(page.shadowRoot?.querySelector(".avatar-upload-btn")).not.toBeNull();
    page.remove();
  });

  test("replacing or dropping the chosen file revokes its object URL", async () => {
    const spy = spyOn(URL, "revokeObjectURL");
    revokeSpy = spy;
    globalThis.fetch = defaultAccountFetch();

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    const input = page.shadowRoot?.querySelector(
      "input[type=file]",
    ) as HTMLInputElement;
    const choose = (name: string) => {
      const file = new File(["x"], name, { type: "image/png" });
      Object.defineProperty(input, "files", {
        value: [file],
        configurable: true,
      });
      input.dispatchEvent(new Event("change"));
    };
    choose("a.png");
    await page.updateComplete;
    const first = page.shadowRoot
      ?.querySelector("img.profile-avatar")
      ?.getAttribute("src");
    choose("b.png");
    await page.updateComplete;
    // Replacing the pick revokes the superseded local preview.
    expect(spy).toHaveBeenCalledWith(first);

    // Teardown revokes the pending preview too.
    page.remove();
    expect(spy.mock.calls.length).toBeGreaterThanOrEqual(2);
  });

  test("a confirmed avatar upload refreshes the profile and dispatches the window event", async () => {
    const uploadCalls: string[] = [];
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url =
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.href
            : input.url;
      if (url.includes("/users/me/avatar")) {
        uploadCalls.push(url);
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: {
            ...PROFILE_BODY,
            avatarUrl:
              "http://localhost:3000/media/images/cccccccccccccccccccccccccccccccc.png",
          },
        });
      }
      if (url.includes("/users/me/favorites/")) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: EMPTY_FAVORITES,
        });
      }
      const sessions = routeSessions(url, EMPTY_SESSIONS);
      if (sessions) return sessions;
      if (url.includes("/users/me")) {
        return mockResponse({ ok: true, status: 200, jsonBody: PROFILE_BODY });
      }
      throw new TypeError(`unrouted fetch in test: ${url}`);
    }) as unknown as typeof fetch;

    let avatarEvents = 0;
    const listener = () => {
      avatarEvents++;
    };
    window.addEventListener("sf-avatar-changed", listener);
    windowCleanups.push(() =>
      window.removeEventListener("sf-avatar-changed", listener),
    );

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    const input = page.shadowRoot?.querySelector(
      "input[type=file]",
    ) as HTMLInputElement;
    const file = new File(["x"], "me.png", { type: "image/png" });
    Object.defineProperty(input, "files", {
      value: [file],
      configurable: true,
    });
    input.dispatchEvent(new Event("change"));
    await page.updateComplete;

    (
      page.shadowRoot?.querySelector(".avatar-upload-btn") as HTMLButtonElement
    ).click();
    await settle();
    await page.updateComplete;

    expect(uploadCalls.length).toBe(1);
    expect(uploadCalls[0]).toContain("/users/me/avatar");
    expect(avatarEvents).toBe(1);
    expect(
      page.shadowRoot?.querySelector("img.profile-avatar")?.getAttribute("src"),
    ).toBe(
      "http://localhost:3000/media/images/cccccccccccccccccccccccccccccccc.png",
    );
    expect(
      page.shadowRoot?.querySelector(".avatar-success")?.textContent?.trim(),
    ).toBe(el.account.avatarSuccess);
    // The result note is a focusable live region — focus moves to it after
    // the success render (the forgot-page pattern).
    const success = page.shadowRoot?.querySelector(".avatar-success");
    expect(success?.getAttribute("role")).toBe("status");
    expect(success?.getAttribute("tabindex")).toBe("-1");
    await waitFor(
      () =>
        !!page.shadowRoot?.activeElement?.classList.contains("avatar-success"),
    );
    page.remove();
  });

  test("a 413 upload maps to the Greek too-large copy and keeps the old avatar", async () => {
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url =
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.href
            : input.url;
      if (url.includes("/users/me/avatar")) {
        return mockResponse({
          ok: false,
          status: 413,
          jsonBody: {
            type: "/problems/bad-request",
            title: "Payload Too Large",
            status: 413,
            violations: [{ field: "file", code: "tooLarge" }],
          },
        });
      }
      if (url.includes("/users/me/favorites/")) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: EMPTY_FAVORITES,
        });
      }
      const sessions = routeSessions(url, EMPTY_SESSIONS);
      if (sessions) return sessions;
      if (url.includes("/users/me")) {
        return mockResponse({ ok: true, status: 200, jsonBody: PROFILE_BODY });
      }
      throw new TypeError(`unrouted fetch in test: ${url}`);
    }) as unknown as typeof fetch;

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    const input = page.shadowRoot?.querySelector(
      "input[type=file]",
    ) as HTMLInputElement;
    const file = new File(["x"], "me.png", { type: "image/png" });
    Object.defineProperty(input, "files", {
      value: [file],
      configurable: true,
    });
    input.dispatchEvent(new Event("change"));
    await page.updateComplete;

    (
      page.shadowRoot?.querySelector(".avatar-upload-btn") as HTMLButtonElement
    ).click();
    await settle();
    await page.updateComplete;

    expect(
      page.shadowRoot?.querySelector(".avatar-error")?.textContent?.trim(),
    ).toBe(el.account.avatarTooLarge);
    // The confirmed avatar is still null — no profile replacement
    // happened. The LOCAL preview (the chosen file) stays visible while
    // the pick awaits a retry; the served image never appeared.
    const img = page.shadowRoot?.querySelector("img.profile-avatar");
    expect(img?.getAttribute("src")?.startsWith("blob:")).toBe(true);
    expect(page.shadowRoot?.querySelector(".avatar-success")).toBeNull();
    page.remove();
  });
});

// ── Email verification + change ────────────────────────────────────

describe("account-page email verification", () => {
  test("an unverified profile renders the banner with the resend button and no change form", async () => {
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : (input as Request).url;
      if (url.includes("/users/me/favorites/")) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: EMPTY_FAVORITES,
        });
      }
      return mockResponse({
        ok: true,
        status: 200,
        jsonBody: { ...PROFILE_BODY, emailVerified: false },
      });
    }) as unknown as typeof fetch;

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    const banner = page.shadowRoot?.querySelector(".verify-banner");
    expect(banner).not.toBeNull();
    expect(banner?.textContent).toContain(el.account.emailUnverifiedLabel);
    const resend = banner?.querySelector("button");
    expect(resend?.textContent?.trim()).toBe(el.account.resendVerification);
    // The change form stays closed until toggled.
    expect(page.shadowRoot?.querySelector(".email-change form")).toBeNull();
  });

  test("a verified profile renders the verified chip and no banner", async () => {
    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    expect(page.shadowRoot?.querySelector(".verify-banner")).toBeNull();
    expect(
      page.shadowRoot?.querySelector(".email-status")?.textContent?.trim(),
    ).toBe(el.account.emailVerifiedLabel);
  });

  test("resend posts to the endpoint and shows the success copy", async () => {
    let resendCalled = false;
    globalThis.fetch = (async (
      input: RequestInfo | URL,
      init?: RequestInit,
    ) => {
      const url = typeof input === "string" ? input : (input as Request).url;
      if (url.includes("/auth/resend-verification")) {
        resendCalled = true;
        expect(init?.method).toBe("POST");
        return mockResponse({ ok: true, status: 204, jsonBody: null });
      }
      if (url.includes("/users/me/favorites/")) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: EMPTY_FAVORITES,
        });
      }
      return mockResponse({
        ok: true,
        status: 200,
        jsonBody: { ...PROFILE_BODY, emailVerified: false },
      });
    }) as unknown as typeof fetch;

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    const resend = page.shadowRoot?.querySelector(
      ".verify-banner button",
    ) as HTMLButtonElement;
    resend.click();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    expect(resendCalled).toBe(true);
    expect(
      page.shadowRoot?.querySelector(".verify-success")?.textContent?.trim(),
    ).toBe(el.account.resendSuccess);
  });

  test("a resend 429 cooldown never disables the password form", async () => {
    globalThis.fetch = (async (
      input: RequestInfo | URL,
      _init?: RequestInit,
    ) => {
      const url = typeof input === "string" ? input : (input as Request).url;
      if (url.includes("/auth/resend-verification")) {
        return mockResponse({
          ok: false,
          status: 429,
          headers: {
            "content-type": "application/problem+json",
            "Retry-After": "60",
          },
          jsonBody: {
            type: "/problems/rate-limited",
            title: "Too many requests",
            status: 429,
          },
        });
      }
      if (url.includes("/users/me/favorites/")) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: EMPTY_FAVORITES,
        });
      }
      return mockResponse({
        ok: true,
        status: 200,
        jsonBody: { ...PROFILE_BODY, emailVerified: false },
      });
    }) as unknown as typeof fetch;

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    (
      page.shadowRoot?.querySelector(
        ".verify-banner button",
      ) as HTMLButtonElement
    ).click();
    // The resend carries its own ticking cooldown…
    await waitFor(() => {
      const resend = page.shadowRoot?.querySelector(
        ".verify-banner button",
      ) as HTMLButtonElement | null;
      return !!resend && resend.disabled;
    });
    const resend = page.shadowRoot?.querySelector(
      ".verify-banner button",
    ) as HTMLButtonElement;
    expect(resend.disabled).toBe(true);

    // …while the password form's submit stays usable: the two windows are
    // separate controllers, and a 30-minute resend bucket must never lock
    // the password change.
    await switchTab(page, el.account.securityTab);
    const submit = page.shadowRoot?.querySelector(
      "#account-panel-security button[type='submit']",
    ) as HTMLButtonElement;
    expect(submit).not.toBeNull();
    expect(submit.disabled).toBe(false);
  });

  test("a server 422 on the email change focuses the first invalid field", async () => {
    globalThis.fetch = (async (
      input: RequestInfo | URL,
      init?: RequestInit,
    ) => {
      const url = typeof input === "string" ? input : (input as Request).url;
      if (url.includes("/users/me/email") && init?.method === "PUT") {
        return mockResponse({
          ok: false,
          status: 422,
          headers: { "content-type": "application/problem+json" },
          jsonBody: {
            type: "/problems/validation",
            title: "Validation failed",
            status: 422,
            violations: [
              { field: "email", code: "alreadyTaken", message: "taken" },
            ],
          },
        });
      }
      if (url.includes("/users/me/favorites/")) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: EMPTY_FAVORITES,
        });
      }
      return mockResponse({ ok: true, status: 200, jsonBody: PROFILE_BODY });
    }) as unknown as typeof fetch;

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    const toggle = page.shadowRoot?.querySelector(
      ".email-change button",
    ) as HTMLButtonElement;
    toggle.click();
    await page.updateComplete;
    fill(page, "account-new-email", "taken@example.com");
    fill(page, "account-email-current-password", "current-pass");
    await page.updateComplete;

    const form = page.shadowRoot?.querySelector(
      ".email-change form",
    ) as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await settle();
    await page.updateComplete;

    // Focus lands AFTER the inputs re-enable (a disabled input cannot take
    // focus) — the sign-in-page ordering.
    expect(page.shadowRoot?.activeElement?.id).toBe("account-new-email");
  });

  test("the change button opens the email form; empty fields block the submit", async () => {
    const calls: string[] = [];
    const baseFetch = defaultAccountFetch();
    mockFetchImpl(async (input, init) => {
      calls.push(typeof input === "string" ? input : input.toString());
      return baseFetch(input, init);
    });

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    const toggle = page.shadowRoot?.querySelector(
      ".email-change button",
    ) as HTMLButtonElement;
    toggle.click();
    await page.updateComplete;

    const form = page.shadowRoot?.querySelector(
      ".email-change form",
    ) as HTMLFormElement;
    expect(form).not.toBeNull();

    // The recovery warning rides the same hint slot as registration —
    // a wrong NEW address kills self-service reset too.
    const hint = page.shadowRoot?.querySelector("#account-new-email-hint");
    expect(hint?.textContent?.trim()).toBe(el.auth.emailRecoveryHint);

    calls.length = 0;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await page.updateComplete;

    expect(
      page.shadowRoot
        ?.querySelector("#account-new-email-error")
        ?.textContent?.trim(),
    ).toBe(el.violations.required);
    // The blocked submit sends nothing (the observable request list).
    expect(calls).toEqual([]);
  });

  test("a successful change PUTs, swaps the profile, and shows the success copy", async () => {
    let changeBody: { email: string; currentPassword: string } | null = null;
    globalThis.fetch = (async (
      input: RequestInfo | URL,
      init?: RequestInit,
    ) => {
      const url = typeof input === "string" ? input : (input as Request).url;
      if (url.includes("/users/me") && init?.method === "PUT") {
        changeBody = JSON.parse(String(init.body)) as {
          email: string;
          currentPassword: string;
        };
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: {
            ...PROFILE_BODY,
            email: "fresh@example.com",
            emailVerified: false,
          },
        });
      }
      const sessions = routeSessions(url, EMPTY_SESSIONS);
      if (sessions) return sessions;
      if (url.includes("/users/me/favorites/")) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: EMPTY_FAVORITES,
        });
      }
      return mockResponse({ ok: true, status: 200, jsonBody: PROFILE_BODY });
    }) as unknown as typeof fetch;

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    (
      page.shadowRoot?.querySelector(
        ".email-change button",
      ) as HTMLButtonElement
    ).click();
    await page.updateComplete;

    fill(page, "account-new-email", "fresh@example.com");
    fill(page, "account-email-current-password", "password123");
    await page.updateComplete;

    submit(page);
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    expect(changeBody).not.toBeNull();
    const sent = changeBody as unknown as {
      email: string;
      currentPassword: string;
    };
    expect(sent).toEqual({
      email: "fresh@example.com",
      currentPassword: "password123",
    });
    expect(
      page.shadowRoot?.querySelector(".email-change")?.textContent,
    ).toContain(el.account.changeEmailSuccess);
    // The profile now shows the new email + the unverified banner (the
    // change reset the stamp).
    expect(page.shadowRoot?.textContent).toContain("fresh@example.com");
    expect(page.shadowRoot?.querySelector(".verify-banner")).not.toBeNull();
  });

  test("consumes the verify-success storage banner once", async () => {
    sessionStorage.setItem("sf-verify-success", "1");
    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    expect(
      page.shadowRoot?.querySelector(".verify-success")?.textContent?.trim(),
    ).toBe(el.auth.verifySuccess);
    expect(sessionStorage.getItem("sf-verify-success")).toBeNull();
  });

  test("consumes the post-registration notice once", async () => {
    sessionStorage.setItem("sf-register-success", "1");
    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    expect(
      page.shadowRoot?.querySelector(".verify-success")?.textContent?.trim(),
    ).toBe(el.account.registrationSuccess);
    expect(sessionStorage.getItem("sf-register-success")).toBeNull();
  });
});

// ── Active sessions ────────────────────────────────────────────────

describe("account-page active sessions", () => {
  test("lists the caller's sessions, badges the current one, and offers no revoke on it", async () => {
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : (input as Request).url;
      if (url.includes("/users/me/sessions")) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: {
            items: [SESSION_ITEM, CURRENT_SESSION_ITEM],
            pageInfo: { hasNextPage: false, endCursor: null, total: 2 },
          },
        });
      }
      return mockResponse({ ok: true, status: 200, jsonBody: PROFILE_BODY });
    }) as unknown as typeof fetch;

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    // The session list lives on the DEVICES panel.
    await switchTab(page, el.ui.sessions);

    const rows = page.shadowRoot?.querySelectorAll(".session-row") ?? [];
    expect(rows.length).toBe(2);
    expect(rows[0]?.querySelector(".session-device")?.textContent?.trim()).toBe(
      "Chrome · Android",
    );
    expect(rows[0]?.querySelector(".session-badge")).toBeNull();
    // The unlabeled current session falls back to the catalog copy and
    // carries the badge instead of a revoke button.
    expect(rows[1]?.querySelector(".session-device")?.textContent?.trim()).toBe(
      el.account.sessionsUnknownDevice,
    );
    expect(rows[1]?.querySelector(".session-badge")?.textContent?.trim()).toBe(
      el.account.sessionsCurrent,
    );
    expect(rows[1]?.querySelector(".session-revoke")).toBeNull();
    // One revoke button: the non-current row.
    expect(page.shadowRoot?.querySelectorAll(".session-revoke").length).toBe(1);
    // The instants render as <time> with the canonical wire values.
    const time = rows[0]?.querySelector("time");
    expect(time?.getAttribute("datetime")).toBe(SESSION_ITEM.createdAt);
    expect(time?.textContent?.trim()).toBeTruthy();
  });

  test("revoking a session sends the CSRF'd DELETE and removes the row", async () => {
    const requests: { url: string; method: string; csrf: string | null }[] = [];
    setCSRFToken("tok");
    globalThis.fetch = (async (
      input: RequestInfo | URL,
      init?: RequestInit,
    ) => {
      const url = typeof input === "string" ? input : (input as Request).url;
      if (url.includes("/users/me/sessions")) {
        if (init?.method === "DELETE") {
          requests.push({
            url,
            method: init.method,
            csrf: new Headers(init.headers).get("X-CSRF-Token"),
          });
          return mockResponse({ ok: true, status: 204, jsonBody: "" });
        }
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: {
            items: [SESSION_ITEM, CURRENT_SESSION_ITEM],
            pageInfo: { hasNextPage: false, endCursor: null, total: 2 },
          },
        });
      }
      return mockResponse({ ok: true, status: 200, jsonBody: PROFILE_BODY });
    }) as unknown as typeof fetch;

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    // The session list lives on the DEVICES panel.
    await switchTab(page, el.ui.sessions);

    const revoke = page.shadowRoot?.querySelector(
      ".session-revoke",
    ) as HTMLButtonElement;
    revoke?.click();
    await settle();
    await page.updateComplete;

    expect(requests.length).toBe(1);
    expect(requests[0]!.method).toBe("DELETE");
    expect(requests[0]!.url).toContain("/users/me/sessions/s2");
    expect(requests[0]!.csrf).toBe("tok");
    // Local removal — no refetch, and the current row survives.
    const ids = [
      ...(page.shadowRoot?.querySelectorAll(".session-row") ?? []),
    ].map((row) => row.getAttribute("data-session-id"));
    expect(ids).toEqual(["s1"]);
  });

  test("a failed revoke shows the action banner and keeps the row", async () => {
    globalThis.fetch = (async (
      input: RequestInfo | URL,
      init?: RequestInit,
    ) => {
      const url = typeof input === "string" ? input : (input as Request).url;
      if (url.includes("/users/me/sessions") && init?.method === "DELETE") {
        return mockResponse({
          ok: false,
          status: 500,
          headers: { "Content-Type": "application/problem+json" },
          jsonBody: {
            type: "/problems/internal-error",
            title: "Internal server error",
            status: 500,
          },
        });
      }
      if (url.includes("/users/me/sessions")) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: {
            items: [SESSION_ITEM, CURRENT_SESSION_ITEM],
            pageInfo: { hasNextPage: false, endCursor: null, total: 2 },
          },
        });
      }
      return mockResponse({ ok: true, status: 200, jsonBody: PROFILE_BODY });
    }) as unknown as typeof fetch;

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    // The session list lives on the DEVICES panel.
    await switchTab(page, el.ui.sessions);

    const revoke = page.shadowRoot?.querySelector(
      ".session-revoke",
    ) as HTMLButtonElement;
    revoke?.click();
    await settle();
    await page.updateComplete;

    expect(bannerText(page, "session-action-error")).toBe(
      el.problems["/problems/internal-error"],
    );
    expect(page.shadowRoot?.querySelectorAll(".session-row").length).toBe(2);
  });

  test("a load failure shows the retry affordance and retry refetches", async () => {
    let calls = 0;
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : (input as Request).url;
      if (url.includes("/users/me/sessions")) {
        calls++;
        if (calls === 1) {
          return mockResponse({
            ok: false,
            status: 500,
            headers: { "Content-Type": "application/problem+json" },
            jsonBody: {
              type: "/problems/internal-error",
              title: "Internal server error",
              status: 500,
            },
          });
        }
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: {
            items: [SESSION_ITEM],
            pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
          },
        });
      }
      return mockResponse({ ok: true, status: 200, jsonBody: PROFILE_BODY });
    }) as unknown as typeof fetch;

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    // The session list lives on the DEVICES panel.
    await switchTab(page, el.ui.sessions);

    expect(bannerText(page, "sessions-error")).toBe(
      el.problems["/problems/internal-error"],
    );
    expect(page.shadowRoot?.querySelectorAll(".session-row").length).toBe(0);

    const retry = page.shadowRoot?.querySelector(
      ".sessions-retry",
    ) as HTMLButtonElement;
    retry?.click();
    await settle();
    await page.updateComplete;

    expect(calls).toBe(2);
    expect(page.shadowRoot?.querySelectorAll(".session-row").length).toBe(1);
  });

  test("the empty state renders when no sessions are live", async () => {
    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    // The session list lives on the DEVICES panel.
    await switchTab(page, el.ui.sessions);

    const empty = page.shadowRoot?.querySelector(".sessions-empty");
    expect(empty?.textContent?.trim()).toBe(el.account.sessionsEmpty);
    // A live region, like the other empty states: the list settling into
    // "nothing live" is announced.
    expect(empty?.getAttribute("role")).toBe("status");
  });

  test("load more appends the next page from the minted cursor", async () => {
    const urls: string[] = [];
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : (input as Request).url;
      if (url.includes("/users/me/sessions")) {
        urls.push(url);
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody:
            urls.length === 1
              ? {
                  items: [SESSION_ITEM],
                  pageInfo: {
                    hasNextPage: true,
                    endCursor: "ss1.cursor",
                    total: 3,
                  },
                }
              : {
                  items: [CURRENT_SESSION_ITEM],
                  pageInfo: { hasNextPage: false, endCursor: null, total: 3 },
                },
        });
      }
      return mockResponse({ ok: true, status: 200, jsonBody: PROFILE_BODY });
    }) as unknown as typeof fetch;

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    // The session list lives on the DEVICES panel.
    await switchTab(page, el.ui.sessions);

    const more = page.shadowRoot?.querySelector(
      ".sessions-more",
    ) as HTMLButtonElement;
    expect(more).not.toBeNull();
    more?.click();
    await settle();
    await page.updateComplete;

    expect(urls[1]).toContain("after=ss1.cursor");
    expect(page.shadowRoot?.querySelectorAll(".session-row").length).toBe(2);
    expect(page.shadowRoot?.querySelector(".sessions-more")).toBeNull();
  });

  test("disables every revoke button while one revoke is in flight", async () => {
    let release: (() => void) | undefined;
    globalThis.fetch = (async (
      input: RequestInfo | URL,
      init?: RequestInit,
    ) => {
      const url = typeof input === "string" ? input : (input as Request).url;
      if (url.includes("/users/me/sessions") && init?.method === "DELETE") {
        return new Promise<Response>((resolve) => {
          release = () => resolve(mockResponse({ ok: true, status: 204 }));
        });
      }
      if (url.includes("/users/me/sessions")) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: {
            items: [SESSION_ITEM, { ...SESSION_ITEM, id: "s3" }],
            pageInfo: { hasNextPage: false, endCursor: null, total: 2 },
          },
        });
      }
      return mockResponse({ ok: true, status: 200, jsonBody: PROFILE_BODY });
    }) as unknown as typeof fetch;

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;
    await switchTab(page, el.ui.sessions);

    const buttons = () =>
      [
        ...(page.shadowRoot?.querySelectorAll(".session-revoke") ?? []),
      ] as HTMLButtonElement[];
    expect(buttons().length).toBe(2);
    buttons()[0]?.click();
    await settle();
    await page.updateComplete;

    // One revoke in flight disables BOTH rows' buttons.
    expect(buttons().every((b) => b.disabled)).toBe(true);
    release?.();
    await settle();
    await page.updateComplete;
    expect(buttons().length).toBe(1);
  });
});

describe("account-page avatar geometry", () => {
  test("the avatar is the 9rem visual anchor, sized the same with and without an image", () => {
    // happy-dom cannot measure layout, so the size is pinned against the CSS
    // source.
    const initials = accountStyles.match(/\.avatar \{([^}]*)\}/s)?.[1] ?? "";
    expect(initials).toContain("width: 9rem");
    expect(initials).toContain("height: 9rem");

    // The uploaded image must not change the geometry.
    const image =
      accountStyles.match(/\.profile-avatar \{([^}]*)\}/s)?.[1] ?? "";
    expect(image).toContain("width: 9rem");
    expect(image).toContain("height: 9rem");
  });
});

describe("account-page phone band", () => {
  test("the section tabs scroll instead of widening the page", () => {
    // Five tabs never fit a phone width, and the row's min-content width
    // would propagate up to the page through the shell's main flex item.
    // The row scrolls and the page may shrink; the tabs themselves never
    // shrink.
    const tabs = accountStyles.match(/\.account-tabs \{([^}]*)\}/s)?.[1] ?? "";
    expect(tabs).toContain("overflow-x: auto");
    expect(tabs).toContain("max-width: 100%");

    const tab = accountStyles.match(/\.account-tab \{([^}]*)\}/s)?.[1] ?? "";
    expect(tab).toContain("flex: 0 0 auto");

    const page =
      accountStyles.match(/\.auth-card\.account-page \{([^}]*)\}/s)?.[1] ?? "";
    expect(page).toContain("min-width: 0");
  });

  test("the profile stacks on the phone band so facts never wrap per character", () => {
    // Beside the 9rem avatar (162px) the facts column had ~146px at 360px
    // and the 7rem `dt` column took 126 of it, so `dd`'s overflow-wrap
    // broke words letter by letter. The profile stacks and the facts
    // column gets the whole width.
    const phone =
      accountStyles.match(/@media \(max-width: 700px\) \{(.*?)\n\}/s)?.[1] ??
      "";
    const profile = phone.match(/\.profile \{([^}]*)\}/)?.[1] ?? "";
    expect(profile).toContain("flex-direction: column");

    // Narrow phones: label-over-value facts get the whole column.
    const narrow =
      accountStyles.match(/@media \(max-width: 560px\) \{(.*?)\n\}/s)?.[1] ??
      "";
    const fact = narrow.match(/\.fact \{([^}]*)\}/)?.[1] ?? "";
    expect(fact).toContain("flex-direction: column");
    const factLabel = narrow.match(/\.fact dt \{([^}]*)\}/)?.[1] ?? "";
    expect(factLabel).toContain("min-width: 0");
  });

  test("a favorites card keeps its remove button INSIDE the card", async () => {
    // The remove button is the card's overlay child: the card's own sheet
    // places it in the row's corner and reserves the text room (pinned in
    // the card's suite), so the page carries neither geometry nor the
    // narrow-band stacking.
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url =
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.href
            : input.url;
      if (url.includes("/users/me/favorites/blog-posts")) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: {
            items: [FAV_ITEM],
            pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
          },
        });
      }
      return mockResponse({ ok: true, status: 200, jsonBody: PROFILE_BODY });
    }) as unknown as typeof fetch;

    const page = render();
    await page.updateComplete;
    await settle();
    await page.updateComplete;
    await switchTab(page, el.favorites.sectionTitle);

    const card = cards(page)[0] as ContentCard;
    await card.updateComplete;
    const remove = card.querySelector(".fav-remove") as HTMLButtonElement;
    expect(remove.getAttribute("slot")).toBe("overlay");
    expect(remove.parentElement).toBe(card);
    // happy-dom fires slotchange only for an assignment that changes AFTER
    // the card's slot exists, so re-append the button to exercise the marker
    // the card keys its corner reservation on (the reservation rule itself
    // lives in the card's sheet).
    card.appendChild(remove);
    await card.updateComplete;
    expect(card.hasAttribute("data-overlay")).toBe(true);

    // The accessible name + title still come from the catalog.
    expect(remove.getAttribute("aria-label")).toBe(el.favorites.removeAria);

    // The card owns the row geometry, so the page's sheet keeps only the
    // button's own look plus the list/empty chrome.
    const removeLook =
      accountStyles.match(/\.fav-remove \{([^}]*)\}/)?.[1] ?? "";
    expect(removeLook).toContain("border-radius: 50%");
    expect(removeLook).not.toContain("position: absolute");
    expect(accountStyles).not.toContain(".fav-thumb");
    expect(accountStyles).not.toContain(".fav-link");
    expect(accountStyles).not.toContain(".fav-body");
    expect(accountStyles).not.toContain("@media (max-width: 480px)");

    // The phone band still pads the page; the card shrinks its own
    // thumbnail and stacks itself in the narrowest band.
    const phone =
      accountStyles.match(/@media \(max-width: 700px\) \{(.*?)\n\}/s)?.[1] ??
      "";
    expect(phone).not.toContain("align-self: flex-end");
    expect(phone).toContain("padding: 1.25rem");

    // The card itself keeps the thumbnail-beside-text row shape at rest.
    expect(card.getAttribute("shape")).toBe("row");
    expect(card.hasAttribute("dense")).toBe(true);
  });

  test("the account page's filled buttons carry white labels", async () => {
    // The accent fill's label is the on-accent token (a surface token would
    // sit near 3:1 in the dark theme); the email-change row shares ONE
    // vocabulary with the shared primary control. Comment-blind: the
    // assertions run against the declarations only, because prose may name
    // retired tokens (the rule-blind-pin lesson).
    const controlStyles = stripCssComments(
      await Bun.file(
        new URL("../../shared/ui/controls.css", import.meta.url),
      ).text(),
    );
    const primary = controlStyles.match(/\.button--primary \{([^}]*)\}/)?.[1];
    expect(primary).toContain("color: var(--sf-on-accent)");
    expect(primary).not.toContain("var(--sf-surface");

    const pushStyles = stripCssComments(
      await Bun.file(
        new URL("../notifications/ui/push-settings.css", import.meta.url),
      ).text(),
    );
    expect(pushStyles).not.toContain("color: var(--sf-surface");

    // The email-change pair: one row that wraps, no-wrap labels, and no
    // component-local button geometry left (the vocabulary owns it).
    const actions =
      accountStyles.match(/\.email-change-actions \{([^}]*)\}/)?.[1] ?? "";
    expect(actions).toContain("flex-wrap: wrap");
    const actionButtons =
      accountStyles.match(/\.email-change-actions button \{([^}]*)\}/)?.[1] ??
      "";
    expect(actionButtons).toContain("white-space: nowrap");
    expect(accountStyles).not.toContain(".secondary-btn {");
  });
});
