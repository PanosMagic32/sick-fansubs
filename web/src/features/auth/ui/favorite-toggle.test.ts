/**
 * Favorite toggle tests — the detail-page heart:
 * anonymous hiding, status load, confirmed toggling, inline errors, the
 * 403 stale-CSRF heal, and the shared toggle debounce.
 */

import { describe, expect, test, beforeEach, afterEach, mock } from "bun:test";

import {
  mockFetchOnce,
  mockResponse,
  mockSessionContext,
  stubDebounceTimers,
} from "@shared/api/test-utils.js";
import { el } from "@shared/catalog/el.js";
import { FAVORITE_CHANGED_EVENT } from "@shared/events.js";

import "./favorite-toggle.js";
import type { FavoriteToggle } from "./favorite-toggle.js";

// ── Helpers ─────────────────────────────────────────────────────────

let timers: ReturnType<typeof stubDebounceTimers>;

/** Run-scoped window listeners/stubs — drained in afterEach (one happy-dom
 * window is shared per bun worker). */
const windowCleanups: Array<() => void> = [];

/** The fetch descriptor this file patches directly — restored in afterEach
 * (bun workers share the window across files). */
const originalFetch = Object.getOwnPropertyDescriptor(globalThis, "fetch");

function mockSession(
  status:
    "initializing" | "anonymous" | "authenticated" | "error" = "authenticated",
) {
  return mockSessionContext({ status, user: { username: "Katakuri" } });
}

async function render(
  overrides: {
    status?: "initializing" | "anonymous" | "authenticated" | "error";
  } = {},
): Promise<FavoriteToggle> {
  const toggle = document.createElement("favorite-toggle") as FavoriteToggle;
  toggle.kind = "blog-posts";
  toggle.contentId = "b1";
  (toggle as any).session = mockSession(overrides.status);
  document.body.appendChild(toggle);
  await toggle.updateComplete;
  await settle();
  await toggle.updateComplete;
  return toggle;
}

async function settle(): Promise<void> {
  await new Promise((r) => setTimeout(r, 10));
}

beforeEach(() => {
  document.body.innerHTML = "";
  timers = stubDebounceTimers();
});

afterEach(() => {
  for (const cleanup of windowCleanups) cleanup();
  windowCleanups.length = 0;
  timers.restore();
  if (originalFetch) {
    Object.defineProperty(globalThis, "fetch", originalFetch);
  } else {
    delete (globalThis as { fetch?: unknown }).fetch;
  }
});

function button(toggle: FavoriteToggle): HTMLButtonElement | null {
  return toggle.shadowRoot?.querySelector(".favorite-toggle") ?? null;
}

// ── Rendering ──────────────────────────────────────────────────────

describe("favorite-toggle", () => {
  test("renders nothing for anonymous visitors", async () => {
    // fetch must not even be called.
    const spy = mock(async () =>
      mockResponse({ ok: true, status: 200, jsonBody: { favorited: false } }),
    );
    globalThis.fetch = spy as unknown as typeof fetch;

    const toggle = await render({ status: "anonymous" });

    expect(button(toggle)).toBeNull();
    expect(spy).not.toHaveBeenCalled();
  });

  test("loads the status and shows the outline heart when not favorited", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { favorited: false },
    });

    const toggle = await render();
    const btn = button(toggle);

    expect(req.url).toBe("/api/v1/users/me/favorites/blog-posts/b1");
    expect(btn?.getAttribute("aria-pressed")).toBe("false");
    expect(btn?.getAttribute("aria-label")).toBe(el.favorites.addAria);
    // The outline heart is the shared stroke SVG — same box as
    // the filled state, which is exactly the reported size bug's fix.
    expect(btn?.querySelector("svg")).not.toBeNull();
    expect(btn?.querySelector("svg")?.getAttribute("fill")).toBe("none");
  });

  test("shows the filled heart with the remove label when favorited", async () => {
    mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { favorited: true },
    });

    const toggle = await render();
    const btn = button(toggle);

    expect(btn?.getAttribute("aria-pressed")).toBe("true");
    expect(btn?.getAttribute("aria-label")).toBe(el.favorites.removeAria);
    expect(btn?.querySelector("svg")?.getAttribute("fill")).toBe(
      "currentColor",
    );
  });

  // ── Toggling ────────────────────────────────────────────────────

  test("click adds the favorite (PUT + flip)", async () => {
    const calls: string[] = [];
    globalThis.fetch = mock(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        calls.push(`${init?.method ?? "GET"}`);
        if (calls.length === 1) {
          // The status load.
          return mockResponse({
            ok: true,
            status: 200,
            jsonBody: { favorited: false },
          });
        }
        return mockResponse({ ok: true, status: 204, jsonBody: "" });
      },
    ) as unknown as typeof fetch;

    const toggle = await render();
    button(toggle)?.click();
    timers.fireAll(); // expire the debounce window (the shared debouncer)
    await settle();
    await toggle.updateComplete;

    expect(calls).toEqual(["GET", "PUT"]);
    expect(button(toggle)?.getAttribute("aria-pressed")).toBe("true");
  });

  test("click removes the favorite (DELETE + flip)", async () => {
    const calls: string[] = [];
    globalThis.fetch = mock(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        calls.push(`${init?.method ?? "GET"}`);
        if (calls.length === 1) {
          return mockResponse({
            ok: true,
            status: 200,
            jsonBody: { favorited: true },
          });
        }
        return mockResponse({ ok: true, status: 204, jsonBody: "" });
      },
    ) as unknown as typeof fetch;

    const toggle = await render();
    button(toggle)?.click();
    timers.fireAll();
    await settle();
    await toggle.updateComplete;

    expect(calls).toEqual(["GET", "DELETE"]);
    expect(button(toggle)?.getAttribute("aria-pressed")).toBe("false");
  });

  test("failure keeps the state and shows the Greek error inline", async () => {
    const calls: string[] = [];
    globalThis.fetch = mock(async () => {
      calls.push("fetch");
      if (calls.length === 1) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: { favorited: false },
        });
      }
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
    }) as unknown as typeof fetch;

    const toggle = await render();
    button(toggle)?.click();
    timers.fireAll();
    await settle();
    await toggle.updateComplete;

    // State unchanged (confirmed updates — no rollback needed), error shown.
    expect(button(toggle)?.getAttribute("aria-pressed")).toBe("false");
    const error = toggle.shadowRoot?.querySelector(".favorite-error");
    expect(error?.getAttribute("role")).toBe("alert");
    expect(error?.textContent?.trim()).toBe(
      el.problems["/problems/internal-error"],
    );
  });

  test("403 revalidates to heal the stale CSRF token", async () => {
    const revalidateSpy = mock(async () => {});
    const calls: string[] = [];
    globalThis.fetch = mock(async () => {
      calls.push("fetch");
      if (calls.length === 1) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: { favorited: false },
        });
      }
      return mockResponse({
        ok: false,
        status: 403,
        headers: { "Content-Type": "application/problem+json" },
        jsonBody: {
          type: "/problems/forbidden",
          title: "Forbidden",
          status: 403,
        },
      });
    }) as unknown as typeof fetch;

    const toggle = document.createElement("favorite-toggle") as FavoriteToggle;
    toggle.kind = "blog-posts";
    toggle.contentId = "b1";
    const session = mockSession();
    session.revalidate = revalidateSpy;
    (toggle as any).session = session;
    document.body.appendChild(toggle);
    await toggle.updateComplete;
    await settle();
    await toggle.updateComplete;

    button(toggle)?.click();
    timers.fireAll();
    await settle();
    await toggle.updateComplete;

    expect(revalidateSpy).toHaveBeenCalledTimes(1);
    // The 403 also renders the Greek problem copy inline.
    expect(
      toggle.shadowRoot?.querySelector(".favorite-error")?.textContent?.trim(),
    ).toBe(el.problems["/problems/forbidden"]);
  });

  test("status failure still renders the outline heart with the error", async () => {
    mockFetchOnce({
      ok: false,
      status: 500,
      headers: { "Content-Type": "application/problem+json" },
      jsonBody: {
        type: "/problems/internal-error",
        title: "Internal server error",
        status: 500,
      },
    });

    const toggle = await render();

    expect(button(toggle)?.getAttribute("aria-pressed")).toBe("false");
    expect(toggle.shadowRoot?.querySelector(".favorite-error")).not.toBeNull();
  });

  test("the heart disables while a mutation is in flight", async () => {
    const calls: string[] = [];
    globalThis.fetch = mock(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        calls.push(init?.method ?? "GET");
        if (calls.length === 1) {
          return mockResponse({
            ok: true,
            status: 200,
            jsonBody: { favorited: false },
          });
        }
        // The PUT never settles — the pending state stays visible.
        return new Promise(() => {});
      },
    ) as unknown as typeof fetch;

    const toggle = await render();
    button(toggle)?.click();
    timers.fireAll();
    await toggle.updateComplete;

    expect(calls).toEqual(["GET", "PUT"]);
    expect(button(toggle)?.disabled).toBe(true);
  });

  test("a CONFIRMED toggle dispatches the count-sync event", async () => {
    const details: unknown[] = [];
    const listener = (e: Event) => {
      details.push((e as CustomEvent).detail);
    };
    window.addEventListener(FAVORITE_CHANGED_EVENT, listener);
    windowCleanups.push(() =>
      window.removeEventListener(FAVORITE_CHANGED_EVENT, listener),
    );

    const calls: string[] = [];
    globalThis.fetch = mock(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        calls.push(`${init?.method ?? "GET"}`);
        if (calls.length === 1) {
          return mockResponse({
            ok: true,
            status: 200,
            jsonBody: { favorited: false },
          });
        }
        return mockResponse({ ok: true, status: 204, jsonBody: "" });
      },
    ) as unknown as typeof fetch;

    const toggle = await render();
    button(toggle)?.click();
    timers.fireAll();
    await settle();
    await toggle.updateComplete;

    expect(calls).toEqual(["GET", "PUT"]);
    expect(details).toEqual([
      { kind: "blog-posts", contentId: "b1", favorited: true },
    ]);
  });

  test("a FAILED toggle emits no event — confirmed updates only", async () => {
    const listener = mock(() => {});
    window.addEventListener(FAVORITE_CHANGED_EVENT, listener);
    windowCleanups.push(() =>
      window.removeEventListener(FAVORITE_CHANGED_EVENT, listener),
    );

    const calls: string[] = [];
    globalThis.fetch = mock(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        calls.push(`${init?.method ?? "GET"}`);
        if (calls.length === 1) {
          return mockResponse({
            ok: true,
            status: 200,
            jsonBody: { favorited: false },
          });
        }
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
      },
    ) as unknown as typeof fetch;

    const toggle = await render();
    button(toggle)?.click();
    timers.fireAll();
    await settle();
    await toggle.updateComplete;

    expect(calls).toEqual(["GET", "PUT"]);
    expect(listener).not.toHaveBeenCalled();
  });

  test("a click-play burst delivers ONE request — the shared debounce", async () => {
    const calls: string[] = [];
    globalThis.fetch = mock(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        calls.push(`${init?.method ?? "GET"}`);
        if (calls.length === 1) {
          return mockResponse({
            ok: true,
            status: 200,
            jsonBody: { favorited: false },
          });
        }
        return mockResponse({ ok: true, status: 204, jsonBody: "" });
      },
    ) as unknown as typeof fetch;

    const toggle = await render();
    const btn = button(toggle);
    // Three rapid clicks open ONE window (last click wins) while the
    // button stays ENABLED — clicks inside the window must register — and
    // reports busy via aria-busy.
    btn?.click();
    expect(timers.open()).toBe(1);
    await toggle.updateComplete;
    expect(btn?.disabled).toBe(false);
    expect(btn?.getAttribute("aria-busy")).toBe("true");
    btn?.click();
    btn?.click();
    expect(timers.open()).toBe(1);

    timers.fireAll();
    await settle();
    await toggle.updateComplete;

    expect(calls).toEqual(["GET", "PUT"]);
    expect(btn?.getAttribute("aria-pressed")).toBe("true");
    expect(btn?.getAttribute("aria-busy")).toBe("false");
  });

  test("an even click burst is a net zero — no mutation request", async () => {
    const calls: string[] = [];
    globalThis.fetch = mock(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        calls.push(`${init?.method ?? "GET"}`);
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: { favorited: false },
        });
      },
    ) as unknown as typeof fetch;

    const toggle = await render();
    const btn = button(toggle);
    btn?.click(); // desired = true
    btn?.click(); // desired = false — net zero
    timers.fireAll();
    await settle();
    await toggle.updateComplete;

    expect(calls).toEqual(["GET"]); // the status load only
    expect(btn?.getAttribute("aria-pressed")).toBe("false");
    expect(btn?.getAttribute("aria-busy")).toBe("false");
  });

  test("a sign-out inside the debounce window delivers nothing", async () => {
    const calls: string[] = [];
    globalThis.fetch = mock(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        calls.push(init?.method ?? "GET");
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: { favorited: false },
        });
      },
    ) as unknown as typeof fetch;

    const toggle = await render();
    button(toggle)?.click();
    expect(timers.open()).toBe(1);

    // A cross-tab sign-out lands mid-window: the open target dies with the
    // session instead of delivering an anonymous mutation.
    (toggle as any).session.state = { status: "anonymous", message: "" };
    toggle.requestUpdate();
    await toggle.updateComplete;
    // The context update cancels the open window outright.
    expect(timers.open()).toBe(0);

    timers.fireAll();
    await settle();
    await toggle.updateComplete;

    expect(calls).toEqual(["GET"]); // the status load only
    expect(button(toggle)).toBeNull();
  });

  test("_deliver drops an anonymous target even without the cancel", async () => {
    const calls: string[] = [];
    globalThis.fetch = mock(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        calls.push(init?.method ?? "GET");
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: { favorited: false },
        });
      },
    ) as unknown as typeof fetch;

    const toggle = await render();
    (toggle as any).session.state = { status: "anonymous", message: "" };
    (toggle as any)._desired = true;
    await (toggle as any)._deliver();
    await toggle.updateComplete;

    expect(calls).toEqual(["GET"]); // the status load only
  });
});
