import { afterEach, beforeEach, describe, expect, test } from "bun:test";

import { resetHappyDOMURL, setHappyDOMURL } from "@shared/api/test-utils.js";

import { type ReactiveControllerHost } from "lit";

import {
  PAGE_SIZES,
  UrlCursorPagingController,
  type UrlCursorPagingOptions,
} from "./url-cursor-paging.js";

/** Minimal ReactiveControllerHost — the controller only needs the
 * registration hooks and requestUpdate for the hasPrevious re-render. */
class FakeHost implements ReactiveControllerHost {
  updates = 0;

  addController(_controller: unknown): void {}
  removeController(_controller: unknown): void {}
  requestUpdate(): void {
    this.updates++;
  }
  updateComplete: Promise<boolean> = Promise.resolve(true);
}

/** History-method stubs: bun shares the happy-dom window between test
 * files in a worker, so every stub installed here MUST be restored in
 * afterEach — a leaked replaceState/back wrapper breaks unrelated files
 * (their redirects run through our shim). */
const originalMethods: Record<string, PropertyDescriptor | undefined> = {};

function stubHistory(
  name: "pushState" | "replaceState" | "back",
  impl: (...args: never[]) => void,
): void {
  if (!(name in originalMethods)) {
    originalMethods[name] = Object.getOwnPropertyDescriptor(
      window.history,
      name,
    );
  }
  Object.defineProperty(window.history, name, {
    configurable: true,
    value: impl,
  });
}

interface LoadCall {
  after: string | null;
  limit: number;
  scroll: boolean;
}

interface PushCall {
  url: string;
  state: unknown;
}

function makeController(
  onLoad: (after: string | null, limit: number, scroll: boolean) => void,
  opts: Partial<UrlCursorPagingOptions> = {},
) {
  const host = new FakeHost();
  const controller = new UrlCursorPagingController(host, {
    path: "/posts",
    navigateEvent: "sf-navigate",
    defaultLimit: 10,
    onLoad,
    nextURL: (after, limit) => `/posts?limit=${limit}&after=${after}`,
    limitURL: (limit) => `/posts?limit=${limit}`,
    ...opts,
  });
  return { host, controller };
}

/** Record window.history.pushState calls AND sync happy-dom's location —
 * happy-dom's pushState does not update location (verified by probe);
 * production browsers do. The page tests carry the same shim. */
function spyPushState(): PushCall[] {
  const calls: PushCall[] = [];
  const original = window.history.pushState.bind(window.history);
  stubHistory("pushState", (state: unknown, unused: string, url?: string) => {
    if (url) {
      calls.push({ url, state });
      const resolved = new URL(url, window.location.href);
      setHappyDOMURL(resolved.href);
    }
    original(state, unused, url);
  });
  return calls;
}

/** Record replaceState calls (size changes) with the same location sync. */
function spyReplaceState(): PushCall[] {
  const calls: PushCall[] = [];
  const original = window.history.replaceState.bind(window.history);
  stubHistory(
    "replaceState",
    (state: unknown, unused: string, url?: string) => {
      if (url) {
        calls.push({ url, state });
        const resolved = new URL(url, window.location.href);
        setHappyDOMURL(resolved.href);
      }
      original(state, unused, url);
    },
  );
  return calls;
}

beforeEach(() => {
  setHappyDOMURL("https://example.com/posts");
});

afterEach(() => {
  document.body.innerHTML = "";
  resetHappyDOMURL();
  // Restore every stubbed history method to its original descriptor —
  // bun shares the window between files, so leaks break unrelated tests.
  for (const [name, descriptor] of Object.entries(originalMethods)) {
    if (descriptor) {
      Object.defineProperty(window.history, name, descriptor);
    } else {
      delete (window.history as unknown as Record<string, unknown>)[name];
    }
  }
  for (const name of Object.keys(originalMethods)) {
    delete originalMethods[name];
  }
});

describe("UrlCursorPagingController", () => {
  test("syncs from the URL on connect — first page, default limit", () => {
    const loads: LoadCall[] = [];
    const { controller } = makeController((after, limit, scroll) =>
      loads.push({ after, limit, scroll }),
    );

    controller.hostConnected();
    expect(loads).toEqual([{ after: null, limit: 10, scroll: true }]);
    expect(controller.hasPrevious).toBe(false);
    controller.hostDisconnected();
  });

  test("syncOnConnect: false skips the mount-time sync — the host drives the first load", () => {
    const loads: LoadCall[] = [];
    const { controller } = makeController(
      (after, limit, scroll) => loads.push({ after, limit, scroll }),
      { syncOnConnect: false },
    );

    // Mounting must NOT load (the admin pages latch the load on the
    // authenticated session — an unconditional sync would double-fetch).
    controller.hostConnected();
    expect(loads).toEqual([]);

    // The host's explicit sync still works (the latch path).
    controller.sync(false);
    expect(loads).toEqual([{ after: null, limit: 10, scroll: false }]);
    controller.hostDisconnected();
  });

  test("mounting directly on a cursor page loads it and disables Previous", () => {
    setHappyDOMURL("https://example.com/posts?limit=20&after=sv1.abc");
    const loads: LoadCall[] = [];
    const { controller } = makeController((after, limit, scroll) =>
      loads.push({ after, limit, scroll }),
    );

    controller.hostConnected();
    expect(loads).toEqual([{ after: "sv1.abc", limit: 20, scroll: true }]);
    // No sfPager marker on a shared/deep-linked URL — Previous must stay
    // disabled (history.back() would leave the app).
    expect(controller.hasPrevious).toBe(false);
    controller.hostDisconnected();
  });

  test("a URL without a cursor is page 1 whatever ordinal it states", () => {
    setHappyDOMURL("https://example.com/posts?page=9");
    const loads: LoadCall[] = [];
    const { controller } = makeController((after, limit, scroll) =>
      loads.push({ after, limit, scroll }),
    );

    controller.hostConnected();
    expect(loads).toEqual([{ after: null, limit: 10, scroll: true }]);
    // The cursor decides the rows: an ordinal without one cannot be honest.
    expect(controller.page).toBe(1);
    controller.hostDisconnected();
  });

  test("an empty after= counts as no cursor", () => {
    setHappyDOMURL("https://example.com/posts?after=&page=4");
    const loads: LoadCall[] = [];
    const { controller } = makeController((after, limit, scroll) =>
      loads.push({ after, limit, scroll }),
    );

    controller.hostConnected();
    expect(loads).toEqual([{ after: null, limit: 10, scroll: true }]);
    expect(controller.page).toBe(1);
    controller.hostDisconnected();
  });

  test("a cursor URL keeps its stated ordinal", () => {
    setHappyDOMURL("https://example.com/posts?limit=10&after=sv1.abc&page=3");
    const loads: LoadCall[] = [];
    const { controller } = makeController((after, limit, scroll) =>
      loads.push({ after, limit, scroll }),
    );

    controller.hostConnected();
    expect(loads).toEqual([{ after: "sv1.abc", limit: 10, scroll: true }]);
    expect(controller.page).toBe(3);
    controller.hostDisconnected();
  });

  test("PAGE_SIZES offers the wire-bound 100 (owner request)", () => {
    expect(PAGE_SIZES).toEqual([10, 20, 50, 100]);
  });

  test("next pushes the cursor URL with the sfPager marker and sets hasPrevious", () => {
    const pushed = spyPushState();
    const loads: LoadCall[] = [];
    const { host, controller } = makeController((after, limit, scroll) =>
      loads.push({ after, limit, scroll }),
    );
    controller.hostConnected();

    controller.next("v1.abc");

    expect(pushed).toEqual([
      { url: "/posts?limit=10&after=v1.abc&page=2", state: { sfPager: true } },
    ]);
    expect(controller.hasPrevious).toBe(true);
    expect(controller.page).toBe(2);
    expect(host.updates).toBe(1); // the Previous button enables
    expect(loads[1]).toEqual({ after: "v1.abc", limit: 10, scroll: true });
    controller.hostDisconnected();
  });

  test("Previous survives a refresh on a cursor page (the state marker persists)", () => {
    spyPushState();
    const firstLoads: LoadCall[] = [];
    const first = makeController((after, limit, scroll) =>
      firstLoads.push({ after, limit, scroll }),
    );
    first.controller.hostConnected();
    first.controller.next("v1.abc");
    expect(first.controller.hasPrevious).toBe(true);
    first.controller.hostDisconnected();

    // "Refresh": a NEW controller instance mounts on the same URL. Session
    // history state survives a reload, so the marker is still there.
    const secondLoads: LoadCall[] = [];
    const second = makeController((after, limit, scroll) =>
      secondLoads.push({ after, limit, scroll }),
    );
    second.controller.hostConnected();
    expect(second.controller.hasPrevious).toBe(true);
    expect(secondLoads[0]).toEqual({
      after: "v1.abc",
      limit: 10,
      scroll: true,
    });
    second.controller.hostDisconnected();
  });

  test("a first-page URL has no marker — hasPrevious stays false", () => {
    const loads: LoadCall[] = [];
    const { controller } = makeController((after, limit, scroll) =>
      loads.push({ after, limit, scroll }),
    );
    controller.hostConnected();
    controller.next("v1.abc");
    expect(controller.hasPrevious).toBe(true);

    // A fresh first-page navigation (pushState without the marker):
    // no earlier page of THIS list is reachable through our history.
    window.history.pushState({}, "", "/posts");
    controller.sync();
    expect(controller.hasPrevious).toBe(false);
    controller.hostDisconnected();
  });

  test("popstate re-syncs without scrolling (the browser owns restoration)", () => {
    const loads: LoadCall[] = [];
    const { controller } = makeController((after, limit, scroll) =>
      loads.push({ after, limit, scroll }),
    );
    controller.hostConnected();

    window.dispatchEvent(new PopStateEvent("popstate"));
    expect(loads[1]).toEqual({ after: null, limit: 10, scroll: false });
    controller.hostDisconnected();
  });

  test("a navigate event re-syncs only when it targets this page's pathname", () => {
    const loads: LoadCall[] = [];
    const { controller } = makeController((after, limit, scroll) =>
      loads.push({ after, limit, scroll }),
    );
    controller.hostConnected();

    // Other pages never re-sync this list — including sibling routes
    // sharing the path prefix (a detail route on the same list).
    window.dispatchEvent(
      new CustomEvent("sf-navigate", { detail: { path: "/about" } }),
    );
    window.dispatchEvent(
      new CustomEvent("sf-navigate", { detail: { path: "/posts/123" } }),
    );
    expect(loads.length).toBe(1);

    // Production order: the shell pushState's the URL FIRST, then the
    // event fires — mirror that so the sync reads the restored URL.
    setHappyDOMURL("https://example.com/posts?limit=20&after=v1.abc");
    window.dispatchEvent(
      new CustomEvent("sf-navigate", {
        detail: { path: "/posts?limit=20&after=v1.abc" },
      }),
    );
    expect(loads.length).toBe(2);
    expect(loads[1]).toEqual({ after: "v1.abc", limit: 20, scroll: true });
    controller.hostDisconnected();
  });

  test("setLimit replaceState's the first-page URL and restarts without a cursor", () => {
    const replaced = spyReplaceState();
    const loads: LoadCall[] = [];
    const { controller } = makeController((after, limit, scroll) =>
      loads.push({ after, limit, scroll }),
    );
    controller.hostConnected();
    controller.next("v1.abc"); // page 2 with a marker
    expect(controller.hasPrevious).toBe(true);

    controller.setLimit(50);

    expect(replaced).toEqual([{ url: "/posts?limit=50", state: {} }]);
    // A size change is always a restart at the first page — the cursor is
    // dropped and the marker is gone (replaceState, not a new entry).
    expect(loads.at(-1)).toEqual({ after: null, limit: 50, scroll: true });
    expect(controller.hasPrevious).toBe(false);
    expect(controller.page).toBe(1);
    controller.hostDisconnected();
  });

  test("setLimit removes the ordinal — a size change restarts at page 1", () => {
    const replaced = spyReplaceState();
    const loads: LoadCall[] = [];
    const { controller } = makeController(
      (after, limit, scroll) => loads.push({ after, limit, scroll }),
      // A host URL that carries a stale ordinal: the controller must drop
      // it rather than trust the builder to have omitted it.
      { limitURL: (limit) => `/posts?limit=${limit}&page=9` },
    );
    setHappyDOMURL("https://example.com/posts?limit=10&after=v1.a&page=3");
    controller.hostConnected();
    expect(controller.page).toBe(3);

    controller.setLimit(20);

    expect(replaced).toEqual([{ url: "/posts?limit=20", state: {} }]);
    expect(controller.page).toBe(1);
    expect(loads.at(-1)).toEqual({ after: null, limit: 20, scroll: true });
    controller.hostDisconnected();
  });

  test("next stamps the URL with the ordinal one past the current page", () => {
    const pushed = spyPushState();
    const loads: LoadCall[] = [];
    const { controller } = makeController((after, limit, scroll) =>
      loads.push({ after, limit, scroll }),
    );
    setHappyDOMURL("https://example.com/posts?limit=10&after=v1.a&page=3");
    controller.hostConnected();

    controller.next("v1.b");

    expect(pushed[0]?.url).toBe("/posts?limit=10&after=v1.b&page=4");
    expect(controller.page).toBe(4);
    expect(loads.at(-1)).toEqual({ after: "v1.b", limit: 10, scroll: true });
    controller.hostDisconnected();
  });

  test("sync reads the page ordinal; anything but an integer ≥ 1 is page 1", () => {
    const loads: LoadCall[] = [];
    const { controller } = makeController((after, limit, scroll) =>
      loads.push({ after, limit, scroll }),
    );

    for (const garbage of ["abc", "2abc", "1.5", "0", "-3", ""]) {
      setHappyDOMURL(`https://example.com/posts?after=v1.a&page=${garbage}`);
      controller.sync();
      expect(controller.page).toBe(1);
    }

    setHappyDOMURL("https://example.com/posts?after=v1.a&page=7");
    controller.sync();
    expect(controller.page).toBe(7);

    // Without a cursor the ordinal is meaningless: the cursor decides the
    // rows, so the first page reads as page 1 whatever the URL states.
    setHappyDOMURL("https://example.com/posts?page=7");
    controller.sync();
    expect(controller.page).toBe(1);
    controller.hostDisconnected();
  });

  test("a garbage URL limit falls back to the default; a valid unoffered one is kept", () => {
    const loads: LoadCall[] = [];
    const { controller } = makeController((after, limit, scroll) =>
      loads.push({ after, limit, scroll }),
    );

    // Garbage must never be forwarded to the API as a page size — strict
    // digits-only parsing rejects trailing junk, exponent/hex spellings,
    // fractions, and overflow.
    for (const garbage of [
      "abc",
      "10abc",
      "1e2",
      "0x10",
      "1.5",
      "0",
      "-5",
      "999999999999",
    ]) {
      setHappyDOMURL(`https://example.com/posts?limit=${garbage}`);
      controller.sync();
      expect(controller.limit).toBe(10);
    }

    setHappyDOMURL("https://example.com/posts?limit=7");
    controller.sync();
    expect(controller.limit).toBe(7); // URL stays the source of truth
    controller.hostDisconnected();
  });

  test("prev is exactly history.back()", () => {
    let backs = 0;
    stubHistory("back", () => {
      backs++;
    });

    const loads: LoadCall[] = [];
    const { controller } = makeController((after, limit, scroll) =>
      loads.push({ after, limit, scroll }),
    );
    controller.hostConnected();

    controller.prev();
    expect(backs).toBe(1);
    expect(loads.length).toBe(1); // popstate owns the re-sync, not prev
    controller.hostDisconnected();
  });
});
