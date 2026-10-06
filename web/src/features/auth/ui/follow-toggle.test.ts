/**
 * Follow toggle tests — the detail-page
 * bell: anonymous hiding, empty-contentId hiding, status load, confirmed
 * toggling, inline errors, the 403 stale-CSRF heal, and the
 * pending-disable (no debounce — a follow is not a like).
 */

import { describe, expect, test, beforeEach, afterEach, mock } from "bun:test";

import {
  mockFetchOnce,
  mockResponse,
  mockSessionContext,
} from "@shared/api/test-utils.js";
import { el } from "@shared/catalog/el.js";

import "./follow-toggle.js";
import type { FollowToggle } from "./follow-toggle.js";

/** The fetch descriptor this file patches directly — restored in afterEach
 * (bun workers share the window across files). */
const originalFetch = Object.getOwnPropertyDescriptor(globalThis, "fetch");

// ── Helpers ─────────────────────────────────────────────────────────

function mockSession(
  status:
    "initializing" | "anonymous" | "authenticated" | "error" = "authenticated",
) {
  return mockSessionContext({ status, user: { username: "Katakuri" } });
}

async function render(
  overrides: {
    status?: "initializing" | "anonymous" | "authenticated" | "error";
    contentId?: string;
  } = {},
): Promise<FollowToggle> {
  const toggle = document.createElement("follow-toggle") as FollowToggle;
  toggle.kind = "blog-posts";
  toggle.contentId = overrides.contentId ?? "b1";
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
});

afterEach(() => {
  if (originalFetch) {
    Object.defineProperty(globalThis, "fetch", originalFetch);
  } else {
    delete (globalThis as { fetch?: unknown }).fetch;
  }
});

function button(toggle: FollowToggle): HTMLButtonElement | null {
  return toggle.shadowRoot?.querySelector(".follow-toggle") ?? null;
}

// ── Rendering ──────────────────────────────────────────────────────

describe("follow-toggle", () => {
  test("renders nothing for anonymous visitors", async () => {
    // fetch must not even be called.
    const spy = mock(async () =>
      mockResponse({ ok: true, status: 200, jsonBody: { following: false } }),
    );
    globalThis.fetch = spy as unknown as typeof fetch;

    const toggle = await render({ status: "anonymous" });

    expect(button(toggle)).toBeNull();
    expect(spy).not.toHaveBeenCalled();
  });

  test("renders nothing for an empty contentId", async () => {
    const spy = mock(async () =>
      mockResponse({ ok: true, status: 200, jsonBody: { following: false } }),
    );
    globalThis.fetch = spy as unknown as typeof fetch;

    const toggle = await render({ contentId: "" });

    expect(button(toggle)).toBeNull();
    expect(spy).not.toHaveBeenCalled();
  });

  test("loads the status and shows the unfollowed bell", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { following: false },
    });

    const toggle = await render();
    const btn = button(toggle);

    expect(req.url).toBe("/api/v1/users/me/follows/blog-posts/b1");
    expect(btn?.getAttribute("aria-pressed")).toBe("false");
    expect(btn?.getAttribute("aria-label")).toBe(el.follows.followAria);
    expect(btn?.querySelector("svg")).not.toBeNull();
  });

  test("shows the followed bell with the unfollow label when following", async () => {
    mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { following: true },
    });

    const toggle = await render();
    const btn = button(toggle);

    expect(btn?.getAttribute("aria-pressed")).toBe("true");
    expect(btn?.getAttribute("aria-label")).toBe(el.follows.unfollowAria);
  });

  test("the bell flips between the outline and filled glyphs — one geometry", async () => {
    // The heart pair's contract applied to the bell: same path, same box,
    // only the fill differs, so the glyph never changes size when it flips.
    let following = true;
    globalThis.fetch = mock(async () =>
      mockResponse({ ok: true, status: 200, jsonBody: { following } }),
    ) as unknown as typeof fetch;

    const followed = await render();
    following = false;
    const unfollowed = await render();

    const glyph = (toggle: FollowToggle) =>
      toggle.shadowRoot?.querySelector(".bell-glyph svg") ?? null;
    const followedGlyph = glyph(followed);
    const unfollowedGlyph = glyph(unfollowed);

    const path = followedGlyph?.querySelector("path")?.getAttribute("d");
    expect(path).toBeTruthy();
    expect(path).toBe(
      unfollowedGlyph?.querySelector("path")?.getAttribute("d"),
    );
    expect(followedGlyph?.getAttribute("fill")).toBe("currentColor");
    expect(unfollowedGlyph?.getAttribute("fill")).toBe("none");
    // The shared 1em box, identically declared on both states.
    for (const g of [followedGlyph, unfollowedGlyph]) {
      expect(g?.getAttribute("viewBox")).toBe("0 0 24 24");
      expect(g?.getAttribute("width")).toBe("1em");
      expect(g?.getAttribute("height")).toBe("1em");
    }
  });

  // ── Toggling ────────────────────────────────────────────────────

  test("click adds the follow (PUT + flip)", async () => {
    const calls: string[] = [];
    globalThis.fetch = mock(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        calls.push(`${init?.method ?? "GET"}`);
        if (calls.length === 1) {
          // The status load.
          return mockResponse({
            ok: true,
            status: 200,
            jsonBody: { following: false },
          });
        }
        return mockResponse({ ok: true, status: 204, jsonBody: "" });
      },
    ) as unknown as typeof fetch;

    const toggle = await render();
    button(toggle)?.click();
    await settle();
    await toggle.updateComplete;

    expect(calls).toEqual(["GET", "PUT"]);
    expect(button(toggle)?.getAttribute("aria-pressed")).toBe("true");
    expect(button(toggle)?.getAttribute("aria-label")).toBe(
      el.follows.unfollowAria,
    );
  });

  test("click removes the follow (DELETE + flip)", async () => {
    const calls: string[] = [];
    globalThis.fetch = mock(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        calls.push(`${init?.method ?? "GET"}`);
        if (calls.length === 1) {
          return mockResponse({
            ok: true,
            status: 200,
            jsonBody: { following: true },
          });
        }
        return mockResponse({ ok: true, status: 204, jsonBody: "" });
      },
    ) as unknown as typeof fetch;

    const toggle = await render();
    button(toggle)?.click();
    await settle();
    await toggle.updateComplete;

    expect(calls).toEqual(["GET", "DELETE"]);
    expect(button(toggle)?.getAttribute("aria-pressed")).toBe("false");
    expect(button(toggle)?.getAttribute("aria-label")).toBe(
      el.follows.followAria,
    );
  });

  test("failure keeps the state and shows the Greek error inline", async () => {
    const calls: string[] = [];
    globalThis.fetch = mock(async () => {
      calls.push("fetch");
      if (calls.length === 1) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: { following: false },
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
    await settle();
    await toggle.updateComplete;

    // State unchanged (confirmed updates — no rollback needed), error shown.
    expect(button(toggle)?.getAttribute("aria-pressed")).toBe("false");
    const error = toggle.shadowRoot?.querySelector(".follow-error");
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
          jsonBody: { following: false },
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

    const toggle = document.createElement("follow-toggle") as FollowToggle;
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
    await settle();
    await toggle.updateComplete;

    expect(revalidateSpy).toHaveBeenCalledTimes(1);
    // The 403 also renders the Greek problem copy inline.
    expect(
      toggle.shadowRoot?.querySelector(".follow-error")?.textContent?.trim(),
    ).toBe(el.problems["/problems/forbidden"]);
  });

  test("status failure still renders the outline bell with the error", async () => {
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
    expect(toggle.shadowRoot?.querySelector(".follow-error")).not.toBeNull();
  });

  test("the bell disables while a mutation is in flight", async () => {
    const calls: string[] = [];
    globalThis.fetch = mock(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        calls.push(init?.method ?? "GET");
        if (calls.length === 1) {
          return mockResponse({
            ok: true,
            status: 200,
            jsonBody: { following: false },
          });
        }
        // The PUT never settles — the pending state stays visible.
        return new Promise(() => {});
      },
    ) as unknown as typeof fetch;

    const toggle = await render();
    button(toggle)?.click();
    await toggle.updateComplete;

    expect(calls).toEqual(["GET", "PUT"]);
    expect(button(toggle)?.disabled).toBe(true);
    expect(button(toggle)?.getAttribute("aria-busy")).toBe("true");
  });
});
