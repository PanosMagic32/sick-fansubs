/**
 * Notifications page tests — the authenticated-floor guard (anonymous
 * redirect, NO role gate), the account-item rendering
 * (actor/verb/target/role/time/result, the
 * null-username labels), the comment_reply rendering (verb + content
 * title + mark-read-and-navigate), the comment_removed rendering (the
 * actorless sentence + navigate-without-fragment), the draft_activity
 * rendering (the per-action verb + the mark-read-and-navigate to the
 * STAFF editor), and the read mutations (item click +
 * mark-all dispatch the badge-refresh event).
 */

import { afterEach, beforeEach, describe, expect, test } from "bun:test";

import { el } from "@shared/catalog/el.js";
import {
  mockFetchImpl,
  mockResponse,
  mockSessionContext,
  resetHappyDOMURL,
  restoreHistoryLocationSync,
  restoreMockFetch,
  setHappyDOMURL,
  shimHistoryLocationSync,
  stripCssComments,
  stubProperty,
  waitFor,
} from "@shared/api/test-utils.js";

import "@features/auth/data-access/session.js";
import "./notifications-page.js";
import { NOTIFICATIONS_CHANGED_EVENT } from "./notifications-page.js";
import type { NotificationsPage } from "./notifications-page.js";
import type { PagerNav } from "@shared/ui/pager-nav.js";

// The stylesheet is read from disk rather than through `?inline`: the
// geometry pins assert the served source, with comments stripped first so
// prose can never satisfy a pin (testing rule 7).
const pageStyles = stripCssComments(
  await Bun.file(new URL("./notifications-page.css", import.meta.url)).text(),
);

/** Run-scoped window listeners — every `sf-navigate` and badge-event
 * listener a test adds is removed in afterEach (one happy-dom window is
 * shared per bun worker). */
const windowCleanups: Array<() => void> = [];

function onNavigate(listener: (path: string) => void): void {
  const handler = (e: Event) =>
    listener((e as CustomEvent<{ path: string }>).detail.path);
  window.addEventListener("sf-navigate", handler);
  windowCleanups.push(() => window.removeEventListener("sf-navigate", handler));
}

function onChanged(listener: () => void): void {
  window.addEventListener(NOTIFICATIONS_CHANGED_EVENT, listener);
  windowCleanups.push(() =>
    window.removeEventListener(NOTIFICATIONS_CHANGED_EVENT, listener),
  );
}

function mockSession(
  status:
    "authenticated" | "anonymous" | "error" | "initializing" = "authenticated",
  role = "moderator",
) {
  return mockSessionContext({ status, user: { role } });
}

/** Install the suite's fetch double through the shared descriptor capture —
 * every install pairs with `restoreMockFetch()` in afterEach. */
function stubFetch(
  handler: (
    url: string,
    method: string,
    init?: RequestInit,
  ) => Promise<{ status: number; body: unknown }>,
) {
  mockFetchImpl(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input.toString();
    const method = init?.method ?? "GET";
    const res = await handler(url, method, init);
    return mockResponse({
      ok: res.status < 400,
      status: res.status,
      headers:
        res.status >= 400 ? { "Content-Type": "application/problem+json" } : {},
      jsonBody: res.body,
    });
  });
}

const LIST_BODY = {
  items: [
    {
      id: "ev2",
      kind: "role_changed",
      result: "failure",
      actorUsername: "Actorus",
      targetUsername: "Targetus",
      targetRole: "moderator",
      createdAt: "2026-08-20T12:00:00.000Z",
      read: false,
    },
    {
      id: "ev1",
      kind: "password_reset",
      result: "success",
      actorUsername: null,
      targetUsername: null,
      targetRole: "user",
      createdAt: "2026-08-19T12:00:00.000Z",
      read: false,
    },
  ],
  pageInfo: { hasNextPage: false, endCursor: null, total: 2 },
};

const COMMENT_REPLY_BODY = {
  items: [
    {
      id: "un1",
      kind: "comment_reply",
      actorUsername: "Eleni",
      contentTitle: "Τίτλος ανάρτησης",
      contentKind: "blog-posts",
      contentId: "b1",
      commentId: "c9",
      createdAt: "2026-08-20T12:00:00.000Z",
      read: false,
    },
    {
      id: "un2",
      kind: "comment_reply",
      actorUsername: null,
      contentTitle: null,
      contentKind: "projects",
      contentId: "p1",
      commentId: "c7",
      createdAt: "2026-08-19T12:00:00.000Z",
      read: false,
    },
  ],
  pageInfo: { hasNextPage: false, endCursor: null, total: 2 },
};

function root(page: NotificationsPage): HTMLElement {
  return page.shadowRoot! as unknown as HTMLElement;
}

/** The draft_activity feed: the three actions, a null actor
 * (system label) and a null title (generic content label) on the middle row. */
const DRAFT_ACTIVITY_BODY = {
  items: [
    {
      id: "dr1",
      kind: "draft_activity",
      actorUsername: "Panos",
      draftAction: "created",
      contentTitle: "Πρόχειρη ανάρτηση",
      contentKind: "blog-posts",
      contentId: "b1",
      createdAt: "2026-09-21T12:00:00.000Z",
      read: false,
    },
    {
      id: "dr2",
      kind: "draft_activity",
      actorUsername: null,
      draftAction: "updated",
      contentTitle: null,
      contentKind: "projects",
      contentId: "p1",
      createdAt: "2026-09-21T11:00:00.000Z",
      read: false,
    },
    {
      id: "dr3",
      kind: "draft_activity",
      actorUsername: "Panos",
      draftAction: "unpublished",
      contentTitle: "Τίτλος",
      contentKind: "blog-posts",
      contentId: "b2",
      createdAt: "2026-09-21T10:00:00.000Z",
      read: false,
    },
  ],
  pageInfo: { hasNextPage: false, endCursor: null, total: 3 },
};

async function render(
  role = "moderator",
  status:
    "authenticated" | "anonymous" | "error" | "initializing" = "authenticated",
): Promise<NotificationsPage> {
  setHappyDOMURL("https://example.com/notifications");
  const page = document.createElement("notifications-page");
  (page as unknown as { session: unknown }).session = mockSession(status, role);
  document.body.appendChild(page);
  await page.updateComplete;
  await new Promise((r) => setTimeout(r, 10));
  await page.updateComplete;
  return page;
}

beforeEach(() => {
  shimHistoryLocationSync();
  window.history.replaceState(null, "", window.location.href);
  document.body.innerHTML = "";
});

afterEach(() => {
  for (const cleanup of windowCleanups) cleanup();
  windowCleanups.length = 0;
  restoreHistoryLocationSync();
  resetHappyDOMURL();
  restoreMockFetch();
  document.body.innerHTML = "";
});

describe("notifications-page", () => {
  test("exactly ONE list fetch per authenticated mount (no double-load)", async () => {
    // Regression pin: with the controller's default mount sync the page fired
    // a request before the session was known (a 401 risk), and the latch's
    // request aborted it — the NS_BINDING_ABORTED double-fetch.
    // syncOnConnect:false + the latch = exactly one request.
    let listFetches = 0;
    stubFetch(async () => {
      listFetches += 1;
      return { status: 200, body: LIST_BODY };
    });

    await render();

    expect(listFetches).toBe(1);
  });

  test("anonymous visitors are redirected to /sign-in before any fetch", async () => {
    const navigations: string[] = [];
    onNavigate((path) => navigations.push(path));
    let fetches = 0;
    stubFetch(async () => {
      fetches += 1;
      return { status: 200, body: LIST_BODY };
    });

    await render("moderator", "anonymous");

    expect(navigations).toEqual(["/sign-in"]);
    expect(fetches).toBe(0);
  });

  test("a plain user renders the feed — the authenticated floor has no role gate", async () => {
    stubFetch(async () => ({ status: 200, body: LIST_BODY }));

    const page = await render("user");

    expect(root(page).querySelectorAll(".notification-item").length).toBe(2);
  });

  test("a moderator renders the feed with catalog copy and labels", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: LIST_BODY };
    });

    const page = await render("moderator");

    expect(urls).toEqual(["/api/v1/notifications?limit=10"]);

    const items = root(page).querySelectorAll(".notification-item");
    expect(items.length).toBe(2);

    // ev2: actor + role_changed verb + target + role chip + failure badge.
    const first = items[0]!;
    expect(
      first.querySelector(".notification-actor")?.textContent?.trim(),
    ).toBe("Actorus");
    expect(first.textContent).toContain(el.notifications.verbRoleChanged);
    expect(
      first.querySelector(".notification-target")?.textContent?.trim(),
    ).toBe("Targetus");
    expect(first.querySelector(".notification-role")?.textContent?.trim()).toBe(
      el.account.roleModerator,
    );
    expect(first.querySelector("time")?.getAttribute("datetime")).toBe(
      "2026-08-20T12:00:00.000Z",
    );
    expect(
      first.querySelector(".notification-result-failure")?.textContent?.trim(),
    ).toBe(el.notifications.resultFailure);

    // ev1: null actor → system label; null target → placeholder.
    const second = items[1]!;
    expect(
      second.querySelector(".notification-actor")?.textContent?.trim(),
    ).toBe(el.notifications.systemActor);
    expect(
      second.querySelector(".notification-target")?.textContent?.trim(),
    ).toBe(el.notifications.missingTarget);
    expect(
      second.querySelector(".notification-result-success")?.textContent?.trim(),
    ).toBe(el.notifications.resultSuccess);
  });

  test("an initializing session neither fetches nor redirects; the authenticated flip loads exactly once", async () => {
    const navigations: string[] = [];
    onNavigate((path) => navigations.push(path));
    let fetches = 0;
    stubFetch(async () => {
      fetches += 1;
      return { status: 200, body: LIST_BODY };
    });

    const page = await render("moderator", "initializing");
    expect(fetches).toBe(0);
    expect(navigations).toEqual([]);

    (page as unknown as { session: unknown }).session =
      mockSession("authenticated");
    page.requestUpdate();
    await waitFor(() => fetches === 1);
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;
    expect(fetches).toBe(1);
    expect(navigations).toEqual([]);
  });

  test("a session revalidate does not re-run the load", async () => {
    let fetches = 0;
    stubFetch(async () => {
      fetches += 1;
      return { status: 200, body: LIST_BODY };
    });

    const page = await render("moderator");
    expect(fetches).toBe(1);

    // A revalidate hands the page a NEW session object with the same status:
    // the latch must not treat that as a fresh mount.
    (page as unknown as { session: unknown }).session =
      mockSession("authenticated");
    page.requestUpdate();
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;
    expect(fetches).toBe(1);
  });

  test("a comment_reply item renders the verb and title; click marks read and navigates to the deep link", async () => {
    const calls: string[] = [];
    stubFetch(async (url, method) => {
      calls.push(`${method} ${url}`);
      if (method === "POST") return { status: 204, body: "" };
      return { status: 200, body: COMMENT_REPLY_BODY };
    });

    let navigated = "";
    onNavigate((path) => (navigated = path));

    const page = await render("user");
    const items = root(page).querySelectorAll(".notification-item");
    const first = items[0] as HTMLButtonElement;
    expect(first.textContent).toContain("Eleni");
    expect(first.textContent).toContain(el.notifications.verbCommentReply);
    expect(first.textContent).toContain("Τίτλος ανάρτησης");
    expect(first.querySelector("time")?.getAttribute("datetime")).toBe(
      "2026-08-20T12:00:00.000Z",
    );

    // The second item: null actor → system label; null title → generic
    // content label.
    const second = items[1] as HTMLButtonElement;
    expect(
      second.querySelector(".notification-actor")?.textContent?.trim(),
    ).toBe(el.notifications.systemActor);
    expect(second.textContent).toContain(el.notifications.genericContent);

    first.click();
    await new Promise((r) => setTimeout(r, 10));

    expect(calls).toContain("POST /api/v1/notifications/un1/read");
    expect(navigated).toBe("/blog/b1#comment-c9");
  });

  test("a heart item renders the heart sentence form; click marks read and navigates", async () => {
    const calls: string[] = [];
    stubFetch(async (url, method) => {
      calls.push(`${method} ${url}`);
      if (method === "POST") return { status: 204, body: "" };
      return {
        status: 200,
        body: {
          items: [
            {
              id: "h1",
              kind: "heart",
              actorUsername: "Eleni",
              contentTitle: "Τίτλος ανάρτησης",
              contentKind: "blog-posts",
              contentId: "b1",
              commentId: "c9",
              createdAt: "2026-08-20T12:00:00.000Z",
              read: false,
            },
          ],
          pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
        },
      };
    });

    let navigated = "";
    onNavigate((path) => (navigated = path));

    const page = await render("user");
    const first = root(page).querySelector(
      ".notification-item",
    ) as HTMLButtonElement;
    // The heart sentence form: "Το σχόλιό σου στο {title} αρέσει σε
    // {actor}" — title wrapped, actor last, no gendered articles.
    expect(first.textContent).toContain(el.notifications.verbHeartOpen);
    expect(first.textContent).toContain("Τίτλος ανάρτησης");
    expect(first.textContent).toContain(el.notifications.verbHeartClose);
    expect(
      first.querySelector(".notification-actor")?.textContent?.trim(),
    ).toBe("Eleni");
    expect(first.querySelector("time")?.getAttribute("datetime")).toBe(
      "2026-08-20T12:00:00.000Z",
    );

    first.click();
    await new Promise((r) => setTimeout(r, 10));

    expect(calls).toContain("POST /api/v1/notifications/h1/read");
    expect(navigated).toBe("/blog/b1#comment-c9");
  });

  test("a comment item renders the comment verb and click navigates to the deep link", async () => {
    const calls: string[] = [];
    stubFetch(async (url, method) => {
      calls.push(`${method} ${url}`);
      if (method === "POST") return { status: 204, body: "" };
      return {
        status: 200,
        body: {
          items: [
            {
              id: "cm1",
              kind: "comment",
              actorUsername: "Eleni",
              contentTitle: "Τίτλος ανάρτησης",
              contentKind: "blog-posts",
              contentId: "b1",
              commentId: "c9",
              createdAt: "2026-08-20T12:00:00.000Z",
              read: false,
            },
          ],
          pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
        },
      };
    });

    let navigated = "";
    onNavigate((path) => (navigated = path));

    const page = await render("user");
    const first = root(page).querySelector(
      ".notification-item",
    ) as HTMLButtonElement;
    // "<actor> σχολίασε στο <title>" — the top-level comment verb.
    expect(first.textContent).toContain("Eleni");
    expect(first.textContent).toContain(el.notifications.verbComment);
    expect(first.textContent).toContain("Τίτλος ανάρτησης");
    expect(first.querySelector("time")?.getAttribute("datetime")).toBe(
      "2026-08-20T12:00:00.000Z",
    );

    first.click();
    await new Promise((r) => setTimeout(r, 10));

    expect(calls).toContain("POST /api/v1/notifications/cm1/read");
    expect(navigated).toBe("/blog/b1#comment-c9");
  });

  test("a content_updated item renders the updated verb and click navigates WITHOUT a fragment", async () => {
    const calls: string[] = [];
    stubFetch(async (url, method) => {
      calls.push(`${method} ${url}`);
      if (method === "POST") return { status: 204, body: "" };
      return {
        status: 200,
        body: {
          items: [
            {
              id: "cu1",
              kind: "content_updated",
              actorUsername: "Eleni",
              contentTitle: "Τίτλος έργου",
              contentKind: "projects",
              contentId: "p1",
              createdAt: "2026-08-20T12:00:00.000Z",
              read: false,
            },
          ],
          pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
        },
      };
    });

    let navigated = "";
    onNavigate((path) => (navigated = path));

    const page = await render("user");
    const first = root(page).querySelector(
      ".notification-item",
    ) as HTMLButtonElement;
    // "<actor> ενημέρωσε το <title>" — the content_updated verb.
    expect(first.textContent).toContain("Eleni");
    expect(first.textContent).toContain(el.notifications.verbContentUpdated);
    expect(first.textContent).toContain("Τίτλος έργου");
    expect(first.querySelector("time")?.getAttribute("datetime")).toBe(
      "2026-08-20T12:00:00.000Z",
    );

    first.click();
    await new Promise((r) => setTimeout(r, 10));

    expect(calls).toContain("POST /api/v1/notifications/cu1/read");
    // The content page itself — no #comment- fragment.
    expect(navigated).toBe("/projects/p1");
  });

  test("a new_content item renders the publish verb and click navigates WITHOUT a fragment", async () => {
    const calls: string[] = [];
    stubFetch(async (url, method) => {
      calls.push(`${method} ${url}`);
      if (method === "POST") return { status: 204, body: "" };
      return {
        status: 200,
        body: {
          items: [
            {
              id: "nc1",
              kind: "new_content",
              actorUsername: "Ελένη",
              contentTitle: "Νέα ανάρτηση",
              contentKind: "blog-posts",
              contentId: "b1",
              createdAt: "2026-08-20T12:00:00.000Z",
              read: false,
            },
          ],
          pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
        },
      };
    });

    let navigated = "";
    onNavigate((path) => (navigated = path));

    const page = await render("user");
    const first = root(page).querySelector(
      ".notification-item",
    ) as HTMLButtonElement;
    // "<actor> δημοσίευσε το <title>" — the new_content verb.
    expect(first.textContent).toContain("Ελένη");
    expect(first.textContent).toContain(el.notifications.verbNewContent);
    expect(first.textContent).toContain("Νέα ανάρτηση");
    expect(first.querySelector("time")?.getAttribute("datetime")).toBe(
      "2026-08-20T12:00:00.000Z",
    );

    first.click();
    await new Promise((r) => setTimeout(r, 10));

    expect(calls).toContain("POST /api/v1/notifications/nc1/read");
    // The content page itself — no #comment- fragment.
    expect(navigated).toBe("/blog/b1");
  });

  test("a comment_removed item renders the actorless sentence and navigates WITHOUT a fragment", async () => {
    const calls: string[] = [];
    stubFetch(async (url, method) => {
      calls.push(`${method} ${url}`);
      if (method === "POST") return { status: 204, body: "" };
      return {
        status: 200,
        body: {
          items: [
            {
              id: "rm1",
              kind: "comment_removed",
              // The server always emits null here (the moderator is never
              // named).
              actorUsername: null,
              contentTitle: "Ένα άρθρο",
              contentKind: "blog-posts",
              contentId: "b1",
              commentId: "c9",
              createdAt: "2026-08-20T12:00:00.000Z",
              read: false,
            },
          ],
          pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
        },
      };
    });

    let navigated = "";
    onNavigate((path) => (navigated = path));

    const page = await render("user");
    const first = root(page).querySelector(
      ".notification-item",
    ) as HTMLButtonElement;
    // "Το σχόλιό σου στο <title> αφαιρέθηκε." — and NO actor element: the
    // moderator is never named.
    expect(first.textContent).toContain(
      el.notifications.verbCommentRemovedOpen,
    );
    expect(first.textContent).toContain(
      el.notifications.verbCommentRemovedClose,
    );
    expect(first.textContent).toContain("Ένα άρθρο");
    expect(root(page).querySelector(".notification-actor")).toBeNull();
    expect(first.querySelector("time")?.getAttribute("datetime")).toBe(
      "2026-08-20T12:00:00.000Z",
    );

    first.click();
    await new Promise((r) => setTimeout(r, 10));

    expect(calls).toContain("POST /api/v1/notifications/rm1/read");
    // The comment is gone — the content page itself, no #comment- fragment.
    expect(navigated).toBe("/blog/b1");
  });

  test("a draft_activity item renders the verb its action picks", async () => {
    stubFetch(async () => ({ status: 200, body: DRAFT_ACTIVITY_BODY }));

    const page = await render("moderator");
    const items = root(page).querySelectorAll(".notification-item");
    expect(items).toHaveLength(3);

    // created: actor + «δημιούργησε το πρόχειρο» + title.
    expect(items[0]?.textContent).toContain("Panos");
    expect(items[0]?.textContent).toContain(el.notifications.verbDraftCreated);
    expect(items[0]?.textContent).toContain("Πρόχειρη ανάρτηση");
    expect(items[0]?.querySelector("time")?.getAttribute("datetime")).toBe(
      "2026-09-21T12:00:00.000Z",
    );

    // updated: null actor → system label; null title → generic label.
    expect(
      items[1]?.querySelector(".notification-actor")?.textContent?.trim(),
    ).toBe(el.notifications.systemActor);
    expect(items[1]?.textContent).toContain(el.notifications.verbDraftUpdated);
    expect(
      items[1]
        ?.querySelector(".notification-content-title")
        ?.textContent?.trim(),
    ).toBe(el.notifications.genericContent);

    // unpublished.
    expect(items[2]?.textContent).toContain(
      el.notifications.verbDraftUnpublished,
    );
  });

  test("a draft_activity click marks read and navigates to the STAFF editor for both content kinds", async () => {
    const calls: string[] = [];
    stubFetch(async (url, method) => {
      calls.push(`${method} ${url}`);
      if (method === "POST") return { status: 204, body: "" };
      return { status: 200, body: DRAFT_ACTIVITY_BODY };
    });

    const navigations: string[] = [];
    onNavigate((path) => navigations.push(path));

    const page = await render("moderator");
    const first = root(page).querySelectorAll(
      ".notification-item",
    )[0] as HTMLButtonElement;
    first.click();
    await new Promise((r) => setTimeout(r, 10));

    // blog-posts → the blog editor, NOT /blog/b1 (the content is a draft).
    expect(calls).toContain("POST /api/v1/notifications/dr1/read");
    expect(navigations).toEqual(["/admin/blog/b1"]);

    const second = root(page).querySelectorAll(
      ".notification-item",
    )[1] as HTMLButtonElement;
    second.click();
    await new Promise((r) => setTimeout(r, 10));

    expect(calls).toContain("POST /api/v1/notifications/dr2/read");
    expect(navigations).toEqual(["/admin/blog/b1", "/admin/projects/p1"]);
  });

  test("an unknown draft action renders no item at all (the worker's refusal, mirrored)", async () => {
    stubFetch(async () => ({
      status: 200,
      body: {
        items: [
          {
            id: "dr9",
            kind: "draft_activity",
            actorUsername: "Panos",
            draftAction: "recycled",
            contentTitle: "Τίτλος",
            contentKind: "blog-posts",
            contentId: "b9",
            createdAt: "2026-09-21T12:00:00.000Z",
            read: false,
          },
        ],
        pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
      },
    }));

    const page = await render("moderator");

    // No wordless or wrong-verb line: the row is dropped whole (the item
    // still exists server-side, so the badge is unaffected).
    expect(root(page).querySelectorAll(".notification-item")).toHaveLength(0);
  });

  test("a failed comment mark-read stays put with the error banner — no navigation", async () => {
    stubFetch(async (_url, method) => {
      if (method === "POST") {
        return {
          status: 500,
          body: {
            type: "/problems/internal-error",
            title: "X",
            status: 500,
            requestId: "rid",
          },
        };
      }
      return { status: 200, body: COMMENT_REPLY_BODY };
    });

    let navigated = "";
    onNavigate((path) => (navigated = path));

    const page = await render("user");
    const first = root(page).querySelector(
      ".notification-item",
    ) as HTMLButtonElement;
    first.click();
    await new Promise((r) => setTimeout(r, 10));

    // The mark-read failed: the error banner shows, the user stays (the
    // item remains a retryable click), and no navigation fired.
    expect(root(page).querySelector("error-banner")).not.toBeNull();
    expect(navigated).toBe("");
  });

  test("the empty feed renders the empty state", async () => {
    stubFetch(async () => ({
      status: 200,
      body: {
        items: [],
        pageInfo: { hasNextPage: false, endCursor: null, total: 0 },
      },
    }));

    const page = await render("moderator");

    expect(root(page).querySelector(".notifications-empty")).not.toBeNull();
    const empty = root(page).querySelector(".notifications-empty");
    expect(empty?.getAttribute("role")).toBe("status");
    expect(root(page).querySelector(".mark-all")).toBeNull();
    // The reserved-footprint wrapper.
    expect(
      root(page).querySelector(".notifications-empty")?.closest(".state-block"),
    ).not.toBeNull();
    // The title renders in every state and stays LEFT; the raw-sheet pin
    // below asserts no centering state exists.
    expect(root(page).querySelector(".notifications-header h1")).not.toBeNull();
  });

  test("a failed load renders the error banner", async () => {
    stubFetch(async () => ({
      status: 500,
      body: {
        type: "/problems/internal-error",
        title: "X",
        status: 500,
        requestId: "rid",
      },
    }));

    const page = await render("moderator");

    expect(root(page).querySelector("error-banner")).not.toBeNull();
  });

  test("item click marks the item read and dispatches the badge event", async () => {
    const calls: string[] = [];
    stubFetch(async (url, method) => {
      calls.push(`${method} ${url}`);
      if (method === "POST") return { status: 204, body: "" };
      return { status: 200, body: LIST_BODY };
    });

    let changed = 0;
    onChanged(() => {
      changed += 1;
    });

    const page = await render("moderator");
    const item = root(page).querySelector(".notification-item");
    (item as HTMLButtonElement).click();
    await new Promise((r) => setTimeout(r, 10));

    expect(calls).toContain("POST /api/v1/notifications/ev2/read");
    expect(changed).toBe(1);
  });

  test("the unread look is an accent bar plus an accent tint, and read rows dim", () => {
    // happy-dom cannot evaluate the cascade, so the read/unread styling is
    // pinned against the CSS source: the unread rule must carry BOTH signals
    // (the 3px accent bar is the shape, the tint the surface) and the tint
    // must be its own token, not the shell's dot-grid colour.
    const unread =
      pageStyles.match(/\.notification-item\.is-unread \{([^}]*)\}/)?.[1] ?? "";
    expect(unread).toContain("border-left: 3px solid var(--sf-accent-light)");
    expect(unread).toContain("background: var(--sf-accent-tint)");
    expect(unread).not.toContain("var(--sf-dot)");

    const read =
      pageStyles.match(/\.notification-item\.is-read \{([^}]*)\}/)?.[1] ?? "";
    expect(read).toContain("opacity:");
  });

  test("clicking an account row flips it to read in place", async () => {
    // An account kind is the one row whose click does NOT navigate, so the
    // confirmed mark-read has to flip the flag locally —
    // otherwise the entry the user just opened keeps its unread look until
    // a reload.
    const body = {
      items: [
        {
          id: "ev9",
          kind: "role_changed",
          result: "success",
          actorUsername: "Actorus",
          targetUsername: "Targetus",
          targetRole: "user",
          createdAt: "2026-08-20T12:00:00.000Z",
          read: false,
        },
      ],
      pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
    };
    stubFetch(async (_url, method) => {
      if (method === "POST") return { status: 204, body: "" };
      return { status: 200, body };
    });

    const navigations: string[] = [];
    onNavigate((path) => navigations.push(path));

    const page = await render("moderator");
    const before = root(page).querySelector(".notification-item")!;
    expect(before.classList.contains("is-unread")).toBe(true);

    (before as HTMLButtonElement).click();
    await new Promise((r) => setTimeout(r, 10));

    const after = root(page).querySelector(".notification-item")!;
    expect(after.classList.contains("is-read")).toBe(true);
    expect(after.classList.contains("is-unread")).toBe(false);
    // An account row's click never navigates — the flip IS the outcome.
    expect(navigations).toEqual([]);
  });

  test("mark all dispatches the badge event after the read-all POST", async () => {
    stubFetch(async (_url, method) => {
      if (method === "POST") return { status: 204, body: "" };
      return { status: 200, body: LIST_BODY };
    });

    let changed = 0;
    onChanged(() => {
      changed += 1;
    });

    const page = await render("moderator");
    const markAll = root(page).querySelector(".mark-all") as HTMLButtonElement;
    expect(markAll).not.toBeNull();
    markAll.click();
    await new Promise((r) => setTimeout(r, 10));

    expect(changed).toBe(1);
  });

  test("Next pages with the server cursor and the pager carries hasNext and the total", async () => {
    const urls: string[] = [];
    const cursorItem = {
      id: "un1",
      kind: "comment_reply",
      actorUsername: "Eleni",
      contentTitle: "Τίτλος",
      contentKind: "blog-posts",
      contentId: "b1",
      commentId: "c1",
      createdAt: "2026-08-20T12:00:00.000Z",
      read: false,
    };
    const pageOne = {
      items: [cursorItem],
      pageInfo: { hasNextPage: true, endCursor: "cur1", total: 15 },
    };
    const pageTwo = {
      items: [cursorItem],
      pageInfo: { hasNextPage: false, endCursor: null, total: 15 },
    };
    stubFetch(async (url) => {
      urls.push(url);
      return {
        status: 200,
        body: url.includes("after=cur1") ? pageTwo : pageOne,
      };
    });

    const page = await render("moderator");
    const pager = root(page).querySelector("pager-nav");
    expect(pager).not.toBeNull();
    expect(pager?.hasNext).toBe(true);
    expect(pager?.hasPrevious).toBe(false);
    expect(pager?.total).toBe(15);

    const scrollCalls: Array<[number, number]> = [];
    windowCleanups.push(
      stubProperty(window, "scrollTo", (x: number, y: number) => {
        scrollCalls.push([x, y]);
      }),
    );
    pager!.dispatchEvent(new CustomEvent("sf-pager-next"));
    await waitFor(() => urls.length === 2);
    await page.updateComplete;
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    // A page turn starts at the top (the mount-time load already ran before
    // the stub, so this call can only be the turn's).
    expect(scrollCalls).toEqual([[0, 0]]);

    // The exact request list: the cursor rides `after`, the ordinal stays
    // out of the API URL.
    expect(urls).toEqual([
      "/api/v1/notifications?limit=10",
      "/api/v1/notifications?limit=10&after=cur1",
    ]);
    // The loading state unmounts the pager, so the post-load state arrives on
    // a fresh element.
    const pagerAfter = root(page).querySelector("pager-nav") as PagerNav;
    expect(pagerAfter.hasNext).toBe(false);
    expect(pagerAfter.total).toBe(15);
    expect(pagerAfter.hasPrevious).toBe(true);
  });

  test("a newer load supersedes the in-flight one", async () => {
    const signals: (AbortSignal | null)[] = [];
    let releaseFirst: (() => void) | null = null;
    stubFetch(async (url, _method, init) => {
      signals.push(init?.signal ?? null);
      if (!url.includes("after=cur1")) {
        await new Promise<void>((resolve) => {
          releaseFirst = resolve;
        });
        // The superseded response is distinct: if it were painted, the row
        // count below would drop to zero.
        return {
          status: 200,
          body: {
            items: [],
            pageInfo: {
              hasNextPage: false,
              endCursor: null,
              total: 0,
            },
          },
        };
      }
      return { status: 200, body: LIST_BODY };
    });

    const page = await render("moderator");
    expect(signals).toHaveLength(1);

    // A same-route navigation re-syncs the paging controller (the router
    // reuses the page element) and the new URL carries a cursor.
    window.history.pushState(null, "", "/notifications?limit=10&after=cur1");
    window.dispatchEvent(
      new CustomEvent("sf-navigate", { detail: { path: "/notifications" } }),
    );
    await waitFor(() => signals.length === 2);
    expect(signals[0]?.aborted).toBe(true);

    releaseFirst!();
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;
    expect(root(page).querySelectorAll(".notification-item")).toHaveLength(2);
  });

  test("teardown aborts the in-flight load", async () => {
    const signals: (AbortSignal | null)[] = [];
    let release: (() => void) | null = null;
    stubFetch(async (_url, _method, init) => {
      signals.push(init?.signal ?? null);
      await new Promise<void>((resolve) => {
        release = resolve;
      });
      return { status: 200, body: LIST_BODY };
    });

    const page = await render("moderator");
    expect(signals).toHaveLength(1);

    page.remove();
    expect(signals[0]?.aborted).toBe(true);

    // Let the abandoned load settle so nothing dangles past the test.
    release!();
    await new Promise((r) => setTimeout(r, 0));
  });

  test("the load rides the composed 15 s deadline", async () => {
    const originalTimeout = AbortSignal.timeout;
    const deadlines: number[] = [];
    windowCleanups.push(
      stubProperty(AbortSignal, "timeout", ((ms: number) => {
        deadlines.push(ms);
        return originalTimeout.call(AbortSignal, ms);
      }) as typeof AbortSignal.timeout),
    );
    stubFetch(async () => ({ status: 200, body: LIST_BODY }));
    await render("moderator");
    expect(deadlines).toEqual([15_000]);
  });

  test("mark all disables and relabels while the POST is in flight", async () => {
    let resolvePost: ((r: { status: number; body: unknown }) => void) | null =
      null;
    stubFetch(async (_url, method) => {
      if (method === "POST") {
        return new Promise((resolve) => {
          resolvePost = resolve;
        });
      }
      return { status: 200, body: LIST_BODY };
    });

    const page = await render("moderator");
    (root(page).querySelector(".mark-all") as HTMLButtonElement).click();
    await waitFor(
      () =>
        root(page).querySelector(".mark-all")?.textContent?.trim() ===
        el.notifications.markAllPending,
    );
    const pending = root(page).querySelector(".mark-all") as HTMLButtonElement;
    expect(pending.disabled).toBe(true);

    resolvePost!({ status: 204, body: "" });
    await waitFor(
      () =>
        root(page).querySelector(".mark-all")?.textContent?.trim() ===
        el.notifications.markAll,
    );
    expect(
      (root(page).querySelector(".mark-all") as HTMLButtonElement).disabled,
    ).toBe(false);
    // The confirmed read-all flipped every row locally.
    expect(
      root(page).querySelectorAll(".notification-item.is-unread"),
    ).toHaveLength(0);
  });

  test("a failed mark-all keeps the rows unread and shows the banner", async () => {
    stubFetch(async (_url, method) => {
      if (method === "POST") {
        return {
          status: 500,
          body: {
            type: "/problems/internal-error",
            title: "X",
            status: 500,
            requestId: "rid",
          },
        };
      }
      return { status: 200, body: LIST_BODY };
    });

    const page = await render("moderator");
    (root(page).querySelector(".mark-all") as HTMLButtonElement).click();
    await waitFor(() => root(page).querySelector("error-banner") !== null);

    expect(
      root(page).querySelectorAll(".notification-item.is-unread"),
    ).toHaveLength(2);
  });

  test("a mark-read in flight disables the row and a second press cannot double-fire", async () => {
    let resolveRead: ((r: { status: number; body: unknown }) => void) | null =
      null;
    const calls: string[] = [];
    stubFetch(async (url, method) => {
      calls.push(`${method} ${url}`);
      if (method === "POST") {
        return new Promise((resolve) => {
          resolveRead = resolve;
        });
      }
      return { status: 200, body: COMMENT_REPLY_BODY };
    });
    const navigations: string[] = [];
    onNavigate((path) => navigations.push(path));

    const page = await render("user");
    const first = root(page).querySelectorAll(
      ".notification-item",
    )[0] as HTMLButtonElement;
    first.click();
    await waitFor(() => first.disabled);

    // Disabled plus the id guard: neither repeat can double-post or
    // double-navigate.
    first.click();
    first.click();
    expect(calls.filter((c) => c.startsWith("POST"))).toHaveLength(1);

    resolveRead!({ status: 204, body: "" });
    await waitFor(() => navigations.length === 1);
    expect(calls.filter((c) => c.startsWith("POST"))).toHaveLength(1);
    expect(navigations).toEqual(["/blog/b1#comment-c9"]);
  });

  test("a click on a redirected page marks read but skips the navigation", async () => {
    const calls: string[] = [];
    stubFetch(async (url, method) => {
      calls.push(`${method} ${url}`);
      if (method === "POST") return { status: 204, body: "" };
      return { status: 200, body: COMMENT_REPLY_BODY };
    });
    let navigations: string[] = [];
    onNavigate((path) => navigations.push(path));

    const page = await render("user");
    // The session dies after the load: the guard redirects and latches.
    (page as unknown as { session: unknown }).session =
      mockSession("anonymous");
    page.requestUpdate();
    await waitFor(() => navigations.includes("/sign-in"));
    navigations = [];

    const first = root(page).querySelector(
      ".notification-item",
    ) as HTMLButtonElement;
    first.click();
    await waitFor(() => calls.some((c) => c.startsWith("POST")));
    await new Promise((r) => setTimeout(r, 10));
    expect(navigations).toEqual([]);
  });
});

describe("notifications-page — read state and deletion", () => {
  /** A mixed feed: one LEDGER row (an account event, read), one unread event
   * row, one read event row. */
  const MIXED_BODY = {
    items: [
      {
        id: "ev1",
        kind: "password_reset",
        result: "success",
        actorUsername: "Actorus",
        targetUsername: "Targetus",
        targetRole: "user",
        createdAt: "2026-08-20T12:00:00.000Z",
        read: true,
      },
      {
        id: "un-read",
        kind: "comment_reply",
        actorUsername: "Eleni",
        contentTitle: "Τίτλος",
        contentKind: "blog-posts",
        contentId: "b1",
        commentId: "c1",
        createdAt: "2026-08-19T12:00:00.000Z",
        read: true,
      },
      {
        id: "un-unread",
        kind: "heart",
        actorUsername: "Eleni",
        contentTitle: "Τίτλος",
        contentKind: "blog-posts",
        contentId: "b1",
        commentId: "c2",
        createdAt: "2026-08-18T12:00:00.000Z",
        read: false,
      },
    ],
    pageInfo: { hasNextPage: false, endCursor: null, total: 3 },
  };

  test("items wear their read state, and every row offers a delete control", async () => {
    stubFetch(async () => ({ status: 200, body: MIXED_BODY }));

    const page = await render("moderator");
    const rows = [...root(page).querySelectorAll(".notification-row")];
    expect(rows).toHaveLength(3);

    const items = rows.map((r) => r.querySelector(".notification-item")!);
    // Ledger row: read, and it carries the delete control like every row.
    expect(items[0]?.classList.contains("is-read")).toBe(true);
    expect(rows[0]?.querySelector(".notification-delete")).not.toBeNull();
    // Event rows: the flag drives the class, and both carry a delete control.
    expect(items[1]?.classList.contains("is-read")).toBe(true);
    expect(items[2]?.classList.contains("is-unread")).toBe(true);
    expect(rows[1]?.querySelector(".notification-delete")).not.toBeNull();
    expect(rows[2]?.querySelector(".notification-delete")).not.toBeNull();
    // The read state reaches assistive tech: only the unread row carries
    // the sr-only indicator.
    expect(items[0]?.querySelector(".sr-only")).toBeNull();
    expect(items[1]?.querySelector(".sr-only")).toBeNull();
    expect(items[2]?.querySelector(".sr-only")?.textContent?.trim()).toBe(
      el.notifications.unreadRow,
    );
  });

  test("the delete control sits INSIDE the entry's card", async () => {
    stubFetch(async () => ({ status: 200, body: MIXED_BODY }));

    const page = await render("moderator");
    const rows = [...root(page).querySelectorAll(".notification-row")];

    // Every row carries the delete control, so every row carries the class
    // the corner reservation hangs off.
    expect(rows[0]?.classList.contains("has-delete")).toBe(true);
    expect(rows[1]?.classList.contains("has-delete")).toBe(true);
    expect(rows[2]?.classList.contains("has-delete")).toBe(true);

    // happy-dom resolves no CSS: the placement is pinned against the source.
    // The row is the positioning context, the control is absolute inside it,
    // and the entry reserves the corner.
    const row = pageStyles.match(/\.notification-row \{([^}]*)\}/)?.[1] ?? "";
    expect(row).toContain("position: relative");
    const del =
      pageStyles.match(/\.notification-delete \{([^}]*)\}/)?.[1] ?? "";
    expect(del).toContain("position: absolute");
    expect(del).toContain("right: 0.5rem");
    expect(del).not.toContain("align-self: flex-end");
    const reserved =
      pageStyles.match(
        /\.notification-row\.has-delete \.notification-item \{([^}]*)\}/,
      )?.[1] ?? "";
    expect(reserved).toContain("padding-right: 3.25rem");

    // The phone band keeps the control inside the row, and the toolbar
    // stacks instead of overflowing.
    const phone =
      pageStyles.match(/@media \(max-width: 700px\) \{(.*?)\n\}/s)?.[1] ?? "";
    expect(phone).not.toContain("align-self: flex-end");
    const header =
      pageStyles.match(/\.notifications-header \{([^}]*)\}/)?.[1] ?? "";
    expect(header).toContain("flex-wrap: wrap");
    // The stylesheet carries no centering state; comments are already
    // stripped at load, so prose naming the retired class cannot satisfy
    // the pin.
    expect(pageStyles).not.toContain("notifications-header-empty");
    // The stacked band puts the title over the verbs (a full-width actions
    // row inside a non-wrapping header would overflow the view).
    expect(phone).toMatch(
      /\.notifications-header \{[^}]*flex-direction: column/,
    );
    const actions =
      phone.match(/\.notifications-actions \{([^}]*)\}/)?.[1] ?? "";
    expect(actions).toContain("width: 100%");
    const actionButtons =
      phone.match(/\.notifications-actions button \{([^}]*)\}/)?.[1] ?? "";
    expect(actionButtons).toContain("white-space: nowrap");
    expect(actionButtons).toContain("min-width: 0");
  });

  test("deleting an event row sends the DELETE, drops the row, and announces the change", async () => {
    const calls: string[] = [];
    stubFetch(async (url, method) => {
      calls.push(`${method} ${url}`);
      if (method === "DELETE") return { status: 204, body: "" };
      return { status: 200, body: MIXED_BODY };
    });

    let changed = 0;
    onChanged(() => {
      changed += 1;
    });

    const page = await render("moderator");
    const row = [...root(page).querySelectorAll(".notification-row")][2]!;
    expect(row.querySelector(".notification-item")?.textContent).toContain(
      el.notifications.verbHeartOpen,
    );

    (row.querySelector(".notification-delete") as HTMLButtonElement).click();
    await new Promise((r) => setTimeout(r, 10));

    expect(calls).toContain("DELETE /api/v1/notifications/un-unread");
    const ids = [...root(page).querySelectorAll(".notification-row")].map((r) =>
      r.querySelector(".notification-item")?.textContent?.trim(),
    );
    expect(ids).toHaveLength(2);
    expect(changed).toBe(1);
  });

  test("the delete control is an icon button that names itself while in flight", async () => {
    let resolveDelete: (r: {
      status: number;
      body: unknown;
    }) => void = () => {};
    stubFetch(async (_url, method) => {
      if (method === "DELETE") {
        // Hold the DELETE open so the in-flight state is observable.
        return new Promise((res) => {
          resolveDelete = res;
        });
      }
      return { status: 200, body: MIXED_BODY };
    });

    const page = await render("moderator");
    const row = [...root(page).querySelectorAll(".notification-row")][2]!;
    const button = row.querySelector(
      ".notification-delete",
    ) as HTMLButtonElement;

    // Icon-only: no text, the glyph is the visual and the accessible name +
    // hover title carry the catalog wording.
    expect(button.textContent?.trim()).toBe("");
    expect(button.querySelector("svg")).not.toBeNull();
    expect(button.getAttribute("aria-label")).toBe(el.notifications.deleteOne);
    expect(button.getAttribute("title")).toBe(el.notifications.deleteOne);

    button.click();
    await new Promise((r) => setTimeout(r, 10));

    const pending = [
      ...root(page).querySelectorAll(".notification-row"),
    ][2]!.querySelector(".notification-delete") as HTMLButtonElement;
    expect(pending.disabled).toBe(true);
    expect(pending.getAttribute("aria-label")).toBe(
      el.notifications.deleteOnePending,
    );
    expect(pending.getAttribute("title")).toBe(
      el.notifications.deleteOnePending,
    );

    resolveDelete({ status: 204, body: "" });
    await new Promise((r) => setTimeout(r, 10));
    expect(root(page).querySelectorAll(".notification-row")).toHaveLength(2);
  });

  test("clear-read is offered while a read event row exists, removes exactly those, and announces the change", async () => {
    const calls: string[] = [];
    stubFetch(async (url, method) => {
      calls.push(`${method} ${url}`);
      if (method === "POST") return { status: 204, body: "" };
      return { status: 200, body: MIXED_BODY };
    });
    let changed = 0;
    onChanged(() => {
      changed += 1;
    });

    const page = await render("moderator");
    const clear = root(page).querySelector(".clear-read") as HTMLButtonElement;
    expect(clear).not.toBeNull();
    expect(clear.textContent?.trim()).toBe(el.notifications.clearRead);

    clear.click();
    await new Promise((r) => setTimeout(r, 10));

    expect(calls).toContain("POST /api/v1/notifications/clear-read");
    // The read event row and the unread one: only the former goes; the ledger
    // row stays (the server never deletes it).
    const remaining = [...root(page).querySelectorAll(".notification-row")];
    expect(remaining).toHaveLength(2);
    expect(
      remaining.some(
        (r) =>
          r
            .querySelector(".notification-item")
            ?.classList.contains("is-unread") === true,
      ),
    ).toBe(true);
    // Nothing read-and-deletable is left, so the control withdraws.
    expect(root(page).querySelector(".clear-read")).toBeNull();
    // A deleted unread row would change the badge; clear-read keeps the
    // contract one code path, so it announces once.
    expect(changed).toBe(1);
  });

  test("a failed clear-read shows the banner and keeps the rows", async () => {
    stubFetch(async (_url, method) => {
      if (method === "POST") {
        return {
          status: 500,
          body: {
            type: "/problems/internal-error",
            title: "X",
            status: 500,
            requestId: "rid",
          },
        };
      }
      return { status: 200, body: MIXED_BODY };
    });

    const page = await render("moderator");
    (root(page).querySelector(".clear-read") as HTMLButtonElement).click();
    await waitFor(() => root(page).querySelector("error-banner") !== null);

    expect(root(page).querySelectorAll(".notification-row")).toHaveLength(3);
  });

  test("mark all read flips the local rows, so clear-read can follow at once", async () => {
    stubFetch(async (_url, method) => {
      if (method === "POST") return { status: 204, body: "" };
      return { status: 200, body: MIXED_BODY };
    });

    const page = await render("moderator");
    (root(page).querySelector(".mark-all") as HTMLButtonElement).click();
    await new Promise((r) => setTimeout(r, 10));

    expect(
      root(page).querySelectorAll(".notification-item.is-unread"),
    ).toHaveLength(0);
    expect(root(page).querySelector(".clear-read")).not.toBeNull();
  });

  test("deleting a ledger row sends the DELETE and drops the row", async () => {
    const calls: string[] = [];
    stubFetch(async (url, method) => {
      calls.push(`${method} ${url}`);
      if (method === "DELETE") return { status: 204, body: "" };
      return { status: 200, body: MIXED_BODY };
    });

    const page = await render("moderator");
    const row = [...root(page).querySelectorAll(".notification-row")][0]!;
    (row.querySelector(".notification-delete") as HTMLButtonElement).click();
    await new Promise((r) => setTimeout(r, 10));

    expect(calls).toContain("DELETE /api/v1/notifications/ev1");
    expect(root(page).querySelectorAll(".notification-row")).toHaveLength(2);
  });

  test("delete-all confirms, sends the POST, empties the feed, and announces the change", async () => {
    const calls: string[] = [];
    stubFetch(async (url, method) => {
      calls.push(`${method} ${url}`);
      if (method === "POST") return { status: 204, body: "" };
      return { status: 200, body: MIXED_BODY };
    });
    windowCleanups.push(stubProperty(window, "confirm", () => true));

    let changed = 0;
    onChanged(() => {
      changed += 1;
    });

    const page = await render("moderator");
    (root(page).querySelector(".delete-all") as HTMLButtonElement).click();
    await new Promise((r) => setTimeout(r, 10));

    expect(calls).toContain("POST /api/v1/notifications/delete-all");
    expect(root(page).querySelectorAll(".notification-row")).toHaveLength(0);
    expect(root(page).querySelector(".notifications-empty")).not.toBeNull();
    expect(changed).toBe(1);
  });

  test("a declined delete-all confirmation sends nothing", async () => {
    const calls: string[] = [];
    stubFetch(async (url, method) => {
      calls.push(`${method} ${url}`);
      return { status: 200, body: MIXED_BODY };
    });
    windowCleanups.push(stubProperty(window, "confirm", () => false));

    const page = await render("moderator");
    (root(page).querySelector(".delete-all") as HTMLButtonElement).click();
    await new Promise((r) => setTimeout(r, 10));

    expect(
      calls.filter((c) => c === "POST /api/v1/notifications/delete-all"),
    ).toEqual([]);
    expect(root(page).querySelectorAll(".notification-row")).toHaveLength(3);
  });

  test("a failed delete-all keeps the feed and shows the mapped error", async () => {
    stubFetch(async (_url, method) => {
      if (method === "POST") {
        return {
          status: 500,
          body: {
            type: "/problems/internal-error",
            title: "X",
            status: 500,
            requestId: "rid",
          },
        };
      }
      return { status: 200, body: MIXED_BODY };
    });
    windowCleanups.push(stubProperty(window, "confirm", () => true));

    const page = await render("moderator");
    (root(page).querySelector(".delete-all") as HTMLButtonElement).click();
    await waitFor(() => root(page).querySelector("error-banner") !== null);

    expect(root(page).querySelectorAll(".notification-row")).toHaveLength(3);
  });

  test("delete-all disables and relabels the verb while the POST is in flight", async () => {
    let resolvePost: ((r: { status: number; body: unknown }) => void) | null =
      null;
    stubFetch(async (_url, method) => {
      if (method === "POST") {
        // Hold the POST open so the in-flight state is observable.
        return new Promise((resolve) => {
          resolvePost = resolve;
        });
      }
      return { status: 200, body: MIXED_BODY };
    });
    windowCleanups.push(stubProperty(window, "confirm", () => true));

    const page = await render("moderator");
    const button = root(page).querySelector(".delete-all") as HTMLButtonElement;
    expect(button.classList.contains("button--danger")).toBe(true);
    expect(button.textContent?.trim()).toBe(el.notifications.deleteAll);

    button.click();
    await new Promise((r) => setTimeout(r, 10));

    const pending = root(page).querySelector(
      ".delete-all",
    ) as HTMLButtonElement;
    expect(pending.disabled).toBe(true);
    expect(pending.textContent?.trim()).toBe(el.notifications.deleteAllPending);

    resolvePost!({ status: 204, body: "" });
    await waitFor(
      () => root(page).querySelectorAll(".notification-row").length === 0,
    );
    expect(root(page).querySelector(".notifications-empty")).not.toBeNull();
  });

  test("delete-all aborts an in-flight page load and resets the paging URL", async () => {
    const signals: (AbortSignal | null)[] = [];
    let releaseLoad: (() => void) | null = null;
    let resolvePost: ((r: { status: number; body: unknown }) => void) | null =
      null;
    const pageOne = {
      items: [MIXED_BODY.items[2]],
      pageInfo: { hasNextPage: true, endCursor: "cur1", total: 3 },
    };
    stubFetch(async (url, method, init) => {
      if (method === "POST") {
        return new Promise((resolve) => {
          resolvePost = resolve;
        });
      }
      signals.push(init?.signal ?? null);
      if (url.includes("after=cur1")) {
        // Hold the page-turn load open so delete-all meets it in flight.
        await new Promise<void>((resolve) => {
          releaseLoad = resolve;
        });
        return { status: 200, body: LIST_BODY };
      }
      return { status: 200, body: pageOne };
    });
    windowCleanups.push(stubProperty(window, "confirm", () => true));

    const page = await render("moderator");
    expect(root(page).querySelectorAll(".notification-row")).toHaveLength(1);

    // The bulk delete rides its own request; while it is out the pager is
    // still live, so a page turn starts a load the delete-all meets.
    (root(page).querySelector(".delete-all") as HTMLButtonElement).click();
    await waitFor(() => resolvePost !== null);
    root(page)
      .querySelector("pager-nav")!
      .dispatchEvent(new CustomEvent("sf-pager-next"));
    await waitFor(() => signals.length === 2);
    expect(window.location.search).toContain("after=cur1");

    resolvePost!({ status: 204, body: "" });
    await waitFor(() => signals[1]?.aborted === true);
    // The cursor pointed at rows the server just removed; the URL restarts
    // from the bare list.
    expect(window.location.search).toBe("");

    // The aborted response must not repaint the feed — and the page must not
    // be left on the spinner the aborted load started.
    releaseLoad!();
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;
    expect(root(page).querySelectorAll(".notification-row")).toHaveLength(0);
    expect(root(page).querySelector(".notifications-empty")).not.toBeNull();
  });
});
