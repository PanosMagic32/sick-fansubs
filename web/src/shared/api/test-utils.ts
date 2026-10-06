/**
 * Test-only helpers shared by the transport suite (client.test.ts), the
 * endpoint suites (feature data-access api tests), and the UI suites
 * (the history-location shim used by URL-syncing page/thread tests).
 *
 * Not imported by production code — the mock shapes here mirror only the
 * parts of Response/RequestInit the client inspects.
 */

import { mock, spyOn } from "bun:test";

import { TOGGLE_DEBOUNCE_DELAY_MS } from "../utils/toggle-debouncer.js";

/** Mock `fetch` return — a partial Response shape the client inspects. */
export interface MockResponse {
  ok: boolean;
  status: number;
  headers?: Record<string, string>;
  jsonBody?: unknown;
}

/** Create a minimal mock Response.
 * `json` returns the raw jsonBody (the problem-parsing path); `text`
 * returns the serialized form — the client's success path reads text and
 * parses it, so bodyless mocks (`jsonBody: ""`) behave like a real
 * empty body. */
export function mockResponse(opts: MockResponse): Response {
  return {
    ok: opts.ok,
    status: opts.status,
    headers: new Headers(opts.headers ?? {}),
    json: async () => opts.jsonBody,
    text: async () =>
      opts.jsonBody === undefined
        ? ""
        : typeof opts.jsonBody === "string"
          ? opts.jsonBody
          : JSON.stringify(opts.jsonBody),
  } as Response;
}

/**
 * Spy on `globalThis.fetch` for one call, returning `res`. Pair every
 * install with restoreMockFetch() in afterEach. Does NOT construct a real
 * Request (happy-dom rejects relative URLs on `about:blank` documents).
 * Extracts fields from the raw fetch args.
 */
export function mockFetchOnce(res: MockResponse): {
  req: {
    method: string;
    headers: Record<string, string>;
    body: string | null;
    formData: FormData | null;
    signal: AbortSignal | null;
    credentials: RequestCredentials;
    url: string;
  };
} {
  const captured = {
    method: "",
    headers: {} as Record<string, string>,
    body: null as string | null,
    formData: null as FormData | null,
    signal: null as AbortSignal | null,
    credentials: "same-origin" as RequestCredentials,
    url: "" as string,
  };

  installFetch(
    mock(async (input: RequestInfo | URL, init?: RequestInit) => {
      captured.url =
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.href
            : input.url;
      captured.method = (init?.method ?? "GET").toUpperCase();
      captured.credentials = init?.credentials ?? "same-origin";
      captured.signal = (init?.signal as AbortSignal) ?? null;
      if (init?.headers) {
        const h = new Headers(init.headers);
        h.forEach((value, key) => {
          captured.headers[key.toLowerCase()] = value;
        });
      }
      if (init?.body instanceof FormData) {
        captured.formData = init.body;
      } else if (typeof init?.body === "string") {
        captured.body = init.body;
      }
      return mockResponse(res);
    }) as unknown as typeof fetch,
  );

  return { req: captured };
}

/**
 * Install a one-off fetch implementation (rejections, raw Response
 * doubles) through the same descriptor capture as mockFetchOnce. Pair
 * every install with restoreMockFetch() in afterEach.
 */
export function mockFetchImpl(
  impl: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>,
): void {
  installFetch(impl as unknown as typeof fetch);
}

let fetchDescriptor: PropertyDescriptor | undefined;
let fetchInstalled = false;

/** Install a fetch double, capturing the descriptor seen at the first
 * install so restoreMockFetch can put it back exactly. */
function installFetch(impl: typeof fetch): void {
  if (!fetchInstalled) {
    fetchDescriptor = Object.getOwnPropertyDescriptor(globalThis, "fetch");
    fetchInstalled = true;
  }
  globalThis.fetch = impl;
}

/**
 * Restore the fetch descriptor captured by the first install — pair every
 * install with this in afterEach (bun workers share the window across
 * files, and an own `undefined` fetch breaks the next file). Idempotent.
 */
export function restoreMockFetch(): void {
  if (!fetchInstalled) return;
  if (fetchDescriptor) {
    Object.defineProperty(globalThis, "fetch", fetchDescriptor);
  } else {
    delete (globalThis as { fetch?: unknown }).fetch;
  }
  fetchInstalled = false;
}

/**
 * happy-dom's history API does not update window.location (production
 * browsers do), so URL-syncing components' pushState/replaceState calls
 * leave the test's `window.location` stale. Install this in beforeEach to
 * route both through happyDOM.setURL.
 *
 * MUST be paired with restoreHistoryLocationSync() in afterEach: bun's test
 * workers run several files sequentially in one process, and a patched
 * history that outlives its file breaks redirect() calls in the next file
 * (Invalid URL against about:blank). A second install without an
 * intervening restore THROWS rather than silently continuing — a missing
 * restore is a bug and must fail loudly.
 */
let historyPatched = false;

export function shimHistoryLocationSync(): void {
  if (historyPatched) {
    throw new Error(
      "shimHistoryLocationSync: history is already patched — pair every install with restoreHistoryLocationSync() in afterEach",
    );
  }
  const sync = (url?: string | URL | null) => {
    if (!url) return;
    // Strict browser mirror: an unresolvable URL THROWS. A navigation from
    // an unset location is a test bug — skipping the sync would mask it.
    const resolved = new URL(String(url), window.location.href);
    setHappyDOMURL(resolved.href);
  };
  // Bound captures: happy-dom's History methods read private fields via
  // `this`, so an unbound copy crashes (this.#browserFrame of undefined).
  const push = window.history.pushState.bind(window.history);
  const replace = window.history.replaceState.bind(window.history);
  Object.defineProperty(window.history, "pushState", {
    configurable: true,
    value: (data: unknown, _unused: string, url?: string | URL | null) => {
      sync(url);
      push(data, _unused, url == null ? undefined : String(url));
    },
  });
  Object.defineProperty(window.history, "replaceState", {
    configurable: true,
    value: (data: unknown, _unused: string, url?: string | URL | null) => {
      sync(url);
      replace(data, _unused, url == null ? undefined : String(url));
    },
  });
  historyPatched = true;
}

/** Undo shimHistoryLocationSync — pair it with the beforeEach install. */
export function restoreHistoryLocationSync(): void {
  if (!historyPatched) {
    return;
  }
  // delete, not redefine: the pristine methods live on the History
  // prototype, so removing our own properties restores them exactly. A
  // redefinition would leave a non-writable own property behind, breaking
  // suites that capture/restore history by assignment (app-shell.test.ts).
  delete (window.history as unknown as { pushState?: unknown }).pushState;
  delete (window.history as unknown as { replaceState?: unknown }).replaceState;
  historyPatched = false;
}

/**
 * Set the shared happy-dom window's URL — the happyDOM.setURL escape
 * hatch, typed once. Files that set a test URL must pair it with
 * resetHappyDOMURL() in afterEach.
 */
export function setHappyDOMURL(url: string): void {
  (
    window as unknown as { happyDOM: { setURL(u: string): void } }
  ).happyDOM.setURL(url);
}

/**
 * Reset the shared happy-dom window's URL to its default (about:blank —
 * the GlobalRegistrator's pristine state). Files that set a test URL must
 * call this in afterEach: bun workers share the window across files, and a
 * leaked https://example.com/ location turns other files' cross-origin
 * assertions into same-origin ones.
 */
export function resetHappyDOMURL(): void {
  setHappyDOMURL("about:blank");
}

/**
 * Spy `AbortSignal.timeout` so a suite can pin a composed deadline's
 * duration. Returns the spy and the paired restore: install per test,
 * restore in `afterEach`.
 */
export function stubAbortSignalTimeout(): {
  spy: ReturnType<typeof spyOn>;
  restore: () => void;
} {
  const spy = spyOn(AbortSignal, "timeout");
  return { spy, restore: () => spy.mockRestore() };
}

/**
 * Shadow-stub one property on the shared happy-dom window/navigator and get
 * a restore function back (call it in afterEach — one window is shared per
 * worker). happy-dom exposes window/navigator fields on the prototype, so
 * the stub is an OWN property that shadows them; the restore puts the
 * original descriptor back, or deletes the own property when there was
 * none. Never leave the shadow in place.
 */
export function stubProperty(
  target: object,
  name: string,
  value: unknown,
): () => void {
  const original = Object.getOwnPropertyDescriptor(target, name);
  // Writable: a test that reassigns the shadow (a second installability
  // state, a changed permission) must not throw in strict mode.
  Object.defineProperty(target, name, {
    value,
    writable: true,
    configurable: true,
  });
  return () => {
    if (original) Object.defineProperty(target, name, original);
    else delete (target as Record<string, unknown>)[name];
  };
}

/**
 * Poll until `condition` holds. Async handlers outlive `updateComplete` and
 * `dispatchEvent` does not await them (a reset submit, an auto-submit on
 * mount, an awaited permission query), so the test must give the handler
 * turns to finish.
 */
export async function waitFor(condition: () => boolean): Promise<void> {
  for (let i = 0; i < 50; i++) {
    if (condition()) return;
    await new Promise((resolve) => setTimeout(resolve, 0));
  }
  throw new Error("waitFor: condition not met");
}

/** Session statuses a mock context can report (mirrors the context union
 * without importing features/** — shared code must not depend on features). */
export type MockSessionStatus =
  "initializing" | "anonymous" | "authenticated" | "error";

/** Overrides accepted by mockSessionContext; every method defaults to a
 * fresh spy. */
export interface MockSessionOverrides {
  status?: MockSessionStatus;
  message?: string;
  user?: { id?: string; username?: string; role?: string };
  mustChangePassword?: boolean;
  signIn?: (identifier: string, password: string) => Promise<void>;
  register?: (
    username: string,
    email: string,
    password: string,
  ) => Promise<void>;
  signOut?: () => Promise<void>;
  signOutAll?: () => Promise<void>;
  changePassword?: (current: string, next: string) => Promise<void>;
  retryBootstrap?: () => void | Promise<void>;
  revalidate?: () => void | Promise<void>;
}

/**
 * Build a mock SessionContext for the UI suites that inject one directly
 * (outside a provider tree). The caller casts at the injection point:
 * test-utils must not import features/**.
 */
export function mockSessionContext(overrides: MockSessionOverrides = {}) {
  return {
    state: {
      status: overrides.status ?? "authenticated",
      message: overrides.message ?? "",
      user: {
        id: overrides.user?.id ?? "u1",
        username: overrides.user?.username ?? "Admin",
        role: overrides.user?.role ?? "user",
      },
      ...(overrides.mustChangePassword === undefined
        ? {}
        : { mustChangePassword: overrides.mustChangePassword }),
    },
    signIn: overrides.signIn ?? mock(async () => {}),
    register: overrides.register ?? mock(async () => {}),
    signOut: overrides.signOut ?? mock(async () => {}),
    signOutAll: overrides.signOutAll ?? mock(async () => {}),
    changePassword: overrides.changePassword ?? mock(async () => {}),
    retryBootstrap: overrides.retryBootstrap ?? mock(async () => {}),
    revalidate: overrides.revalidate ?? mock(async () => {}),
  };
}

/**
 * Capture the path of the shell's sf-navigate events. Pair with the
 * returned remove() in afterEach, or hand a cleanup list the suite already
 * drains.
 */
export function listenForNavigation(cleanups?: Array<() => void>): {
  readonly path: string | null;
  remove: () => void;
} {
  let path: string | null = null;
  const listener = ((e: Event) => {
    path = (e as CustomEvent<{ path: string }>).detail.path;
  }) as EventListener;
  window.addEventListener("sf-navigate", listener);
  const remove = () => window.removeEventListener("sf-navigate", listener);
  cleanups?.push(remove);
  return {
    get path() {
      return path;
    },
    remove,
  };
}

/**
 * Timer stub for the shared ToggleDebouncer: setTimeout calls with the
 * debounce delay are captured instead of scheduled, and clearTimeout
 * removes the matching captured entry — fireAll() runs exactly what a
 * real browser would still deliver after the cancels. Restore in
 * afterEach (the shared happy-dom window leaks otherwise); a second
 * install without an intervening restore THROWS.
 */
let debounceTimersPatched = false;

export function stubDebounceTimers(delayMs = TOGGLE_DEBOUNCE_DELAY_MS): {
  open: () => number;
  fireAll: () => void;
  restore: () => void;
} {
  if (debounceTimersPatched) {
    throw new Error(
      "stubDebounceTimers: timers are already stubbed — pair every install with restore() in afterEach",
    );
  }
  debounceTimersPatched = true;
  const originalST = window.setTimeout;
  const originalCT = window.clearTimeout;
  const timers = new Map<number, () => void>();
  let nextId = 1;

  Object.defineProperty(window, "setTimeout", {
    value: (fn: () => void, ms: number) => {
      if (ms !== delayMs) return originalST(fn, ms);
      const id = nextId++;
      timers.set(id, fn);
      return id;
    },
    writable: true,
    configurable: true,
  });
  Object.defineProperty(window, "clearTimeout", {
    value: (id: number) => {
      if (typeof id === "number" && timers.has(id)) {
        timers.delete(id);
        return;
      }
      originalCT(id);
    },
    writable: true,
    configurable: true,
  });

  return {
    open: () => timers.size,
    fireAll() {
      const fns = [...timers.values()];
      timers.clear();
      for (const fn of fns) fn();
    },
    restore() {
      debounceTimersPatched = false;
      Object.defineProperty(window, "setTimeout", {
        value: originalST,
        writable: true,
        configurable: true,
      });
      Object.defineProperty(window, "clearTimeout", {
        value: originalCT,
        writable: true,
        configurable: true,
      });
    },
  };
}

/**
 * Strip CSS comments before a source pin matches: prose may name a retired
 * value, and a pin must never pass or fail on a comment (web testing rule
 * 7). Apply after the raw file read.
 */
export function stripCssComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, "");
}

/**
 * Strip JavaScript/TypeScript comments for source-level pins — block
 * comments plus `//` line comments (a `://` sequence is left alone so URLs
 * in strings survive). A pin must never pass or fail on prose; this is not
 * a parser.
 */
export function stripSourceComments(source: string): string {
  return source
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/(^|[^:\w])\/\/.*$/gm, "$1");
}
