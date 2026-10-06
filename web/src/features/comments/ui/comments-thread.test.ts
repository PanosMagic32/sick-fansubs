/**
 * Comments thread tests — the inline detail-page
 * section: the list+count load, sort URL sync, anonymous/authenticated
 * composers, create/reply through the 201 Location focus jump, hearts
 * (the confirmed-update machine), author edit, author/moderator
 * delete with the hard-cascade removal, load-more on hasNextPage, and
 * the `#comment-<id>` deep-link contract (chip + highlight, deleted
 * target notice).
 */

import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";

import { clearCSRFToken, setCSRFToken } from "@shared/api/client.js";
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
  stubDebounceTimers,
  stubProperty,
} from "@shared/api/test-utils.js";

import "./comments-thread.js";
import { COMMENT_COUNT_CHANGED_EVENT } from "@shared/events.js";
import type { CommentsThread } from "./comments-thread.js";
import type { CommentItem } from "../data-access/types.js";

// ── Helpers ─────────────────────────────────────────────────────────

/** The raw composer sheet — happy-dom resolves no CSS, so the composer's
 * layout rules are pinned against the stylesheet text. */
const threadStyles = await Bun.file(
  new URL("./comments-thread.css", import.meta.url),
).text();

let timers: ReturnType<typeof stubDebounceTimers>;

/** Run-scoped window listeners/stubs — drained in afterEach (one happy-dom
 * window is shared per bun worker). */
const windowCleanups: Array<() => void> = [];

function mockSession(
  status:
    "initializing" | "anonymous" | "authenticated" | "error" = "authenticated",
  overrides: { id?: string; username?: string; role?: string } = {},
) {
  return mockSessionContext({
    status,
    user: {
      id: overrides.id ?? "u1",
      username: overrides.username ?? "Katakuri",
      role: overrides.role ?? "user",
    },
  });
}

/** The shared happy-dom history shim (test-utils, graduated from the
 * notifications-page probe) — the thread's sort and focus flows both
 * write the URL. */
function stubFetch(
  handler: (
    url: string,
    method: string,
  ) => Promise<{
    status: number;
    body: unknown;
    headers?: Record<string, string>;
  }>,
) {
  mockFetchImpl(async (input, init) => {
    const url = typeof input === "string" ? input : input.toString();
    const method = init?.method ?? "GET";
    const res = await handler(url, method);
    return mockResponse({
      ok: res.status < 400,
      status: res.status,
      headers:
        res.status >= 400
          ? { "Content-Type": "application/problem+json" }
          : (res.headers ?? {}),
      jsonBody: res.body,
    });
  });
}

function author(overrides: Partial<CommentItem["author"]> = {}) {
  return {
    id: "u1",
    username: "Katakuri",
    avatarUrl: null,
    isStaff: false,
    ...overrides,
  };
}

function commentItem(overrides: Partial<CommentItem> = {}): CommentItem {
  return {
    id: "c1",
    body: "Πρώτο σχόλιο",
    author: author(),
    createdAt: "2026-08-28T10:00:00.000Z",
    updatedAt: "2026-08-28T10:00:00.000Z",
    heartsCount: 0,
    hearted: false,
    replies: [],
    ...overrides,
  };
}

/** A reply item per the wire: no `replies` key (the depth discriminator)
 * and none of the reply-window fields. */
function replyItem(id: string): CommentItem {
  return {
    id,
    body: `απάντηση ${id}`,
    author: author({ id: "u2", username: "Eleni" }),
    createdAt: "2026-08-28T10:05:00.000Z",
    updatedAt: "2026-08-28T10:05:00.000Z",
    heartsCount: 0,
    hearted: false,
  };
}

const LIST_BODY = {
  items: [
    commentItem({
      body: "Πρώτο σχόλιο",
      heartsCount: 3,
      replies: [
        {
          // A reply item — the wire OMITS `replies` (the depth
          // discriminator).
          id: "r1",
          body: "Μια απάντηση",
          author: author({ id: "u2", username: "Eleni", isStaff: true }),
          createdAt: "2026-08-28T10:05:00.000Z",
          updatedAt: "2026-08-28T11:00:00.000Z",
          heartsCount: 1,
          hearted: false,
        },
      ],
    }),
    commentItem({
      id: "c2",
      body: "Δεύτερο σχόλιο",
      author: author({ id: "u3", username: "Dimitra" }),
    }),
  ],
  pageInfo: { hasNextPage: false, endCursor: null, total: 2 },
};

const EMPTY_BODY = {
  items: [],
  pageInfo: { hasNextPage: false, endCursor: null, total: 0 },
};

function root(thread: CommentsThread): HTMLElement {
  return thread.shadowRoot! as unknown as HTMLElement;
}

function text(node: HTMLElement, selector: string): string {
  return (node.querySelector(selector)?.textContent ?? "")
    .replace(/\s+/g, " ")
    .trim();
}

/** The row actions are ICON buttons: their identity is the
 * accessible name, not their text — the glyph carries no wording. */
function actionLabels(scope: Element): string[] {
  return [...scope.querySelectorAll(".ct-action")].map(
    (b) => b.getAttribute("aria-label") ?? "",
  );
}

function actionByLabel(
  scope: Element,
  label: string,
): HTMLButtonElement | undefined {
  return [...scope.querySelectorAll(".ct-action")].find(
    (b) => b.getAttribute("aria-label") === label,
  ) as HTMLButtonElement | undefined;
}

async function settle(): Promise<void> {
  await new Promise((r) => setTimeout(r, 10));
}

async function renderThread(
  overrides: {
    kind?: "blog-posts" | "projects";
    contentId?: string;
    status?: "initializing" | "anonymous" | "authenticated" | "error";
    session?: { id?: string; username?: string; role?: string };
  } = {},
): Promise<CommentsThread> {
  const thread = document.createElement("comments-thread") as CommentsThread;
  thread.kind = overrides.kind ?? "blog-posts";
  thread.contentId = overrides.contentId ?? "b1";
  (thread as unknown as { session: unknown }).session = mockSession(
    overrides.status,
    overrides.session,
  );
  document.body.appendChild(thread);
  await thread.updateComplete;
  await settle();
  await thread.updateComplete;
  return thread;
}

beforeEach(() => {
  shimHistoryLocationSync();
  setHappyDOMURL("https://example.com/blog/b1");
  window.history.replaceState(null, "", window.location.href);
  document.body.innerHTML = "";
  timers = stubDebounceTimers();
});

afterEach(() => {
  for (const cleanup of windowCleanups) cleanup();
  windowCleanups.length = 0;
  timers.restore();
  restoreHistoryLocationSync();
  resetHappyDOMURL();
  clearCSRFToken?.();
  mock.restore();
  restoreMockFetch();
  document.body.innerHTML = "";
});

// ── Load / render ───────────────────────────────────────────────────

describe("comments-thread", () => {
  test("loads the list and count — title, nested replies, badge, edited marker, anonymous hearts", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 3 } };
      return { status: 200, body: LIST_BODY };
    });

    const thread = await renderThread({ status: "anonymous" });

    expect(urls).toEqual([
      "/api/v1/blog-posts/b1/comments?sort=top&limit=20",
      "/api/v1/blog-posts/b1/comments/count",
    ]);

    // Count = top-level + replies: 2 + 1.
    expect(text(root(thread), ".ct-title")).toBe("Σχόλια (3)");

    const comments = [...root(thread).querySelectorAll(".ct-comment")];
    expect(comments.length).toBe(3);
    // The reply nests inside its parent and carries the staff badge + the
    // edited marker (updatedAt strictly after createdAt).
    const reply = root(thread).querySelector(".ct-reply");
    expect(reply).not.toBeNull();
    expect(text(reply as HTMLElement, ".ct-name")).toBe("Eleni");
    expect(text(reply as HTMLElement, ".ct-badge")).toBe(
      el.comments.staffBadge,
    );
    expect(text(reply as HTMLElement, ".ct-edited")).toContain(
      el.comments.edited,
    );

    // Anonymous: hearts render as plain counts, no heart buttons, and the
    // composer shows the sign-in prompt.
    expect(root(thread).querySelector(".ct-hearts")).not.toBeNull();
    expect(root(thread).querySelector(".ct-heart")).toBeNull();
    expect(text(root(thread), ".ct-signin-prompt a")).toBe(
      el.comments.signInLink,
    );

    // Reply action only on the two TOP-LEVEL comments — never on the
    // reply (one reply level).
    const replyActions = [
      ...root(thread).querySelectorAll(".ct-action"),
    ].filter((b) => b.getAttribute("aria-label") === el.comments.reply);
    expect(replyActions).toHaveLength(2);
    expect(root(thread).querySelector(".ct-more")).toBeNull();
  });

  test("renders bodies as text plus the emoticon glyphs — never markup", async () => {
    const withEmoticons = {
      items: [commentItem({ id: "c9", body: "γεια :) <b>όχι</b> <3" })],
      pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
    };
    stubFetch(async (url) => {
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 1 } };
      return { status: 200, body: withEmoticons };
    });

    const thread = await renderThread({ status: "anonymous" });

    const textEl = root(thread).querySelector(".ct-text") as HTMLElement;
    // Markup typed into a body stays text — no <b> element exists.
    expect(textEl.querySelector("b")).toBeNull();
    expect(textEl.textContent).toContain("<b>όχι</b>");
    // The two ruled tokens became labeled, colored glyphs.
    const glyphs = [...textEl.querySelectorAll("svg.ct-emoticon")];
    expect(glyphs).toHaveLength(2);
    expect(glyphs[0]!.getAttribute("aria-label")).toBe(
      el.comments.emoticonSmile,
    );
    expect(glyphs[1]!.getAttribute("aria-label")).toBe(
      el.comments.emoticonHeart,
    );
    expect(textEl.textContent).toContain("γεια");
  });

  test("the staff badge reads the derived role: class, title, accessible name", async () => {
    const withStaff = {
      items: [
        commentItem({
          id: "cm",
          author: author({
            id: "u2",
            username: "Moddy",
            isStaff: true,
            staffRole: "moderator",
          }),
        }),
        commentItem({
          id: "ca",
          author: author({
            id: "u3",
            username: "Admy",
            isStaff: true,
            staffRole: "admin",
          }),
        }),
        commentItem({
          id: "cs",
          author: author({
            id: "u4",
            username: "Sadmy",
            isStaff: true,
            staffRole: "super-admin",
          }),
        }),
        // A staff author with no role at all: the defensive neutral badge.
        commentItem({
          id: "cn",
          author: author({
            id: "u6",
            username: "Legacy",
            isStaff: true,
            staffRole: undefined,
          }),
        }),
        commentItem({
          id: "cu",
          author: author({ id: "u5", username: "Alice" }),
        }),
      ],
      pageInfo: { hasNextPage: false, endCursor: null, total: 5 },
    };
    stubFetch(async (url) => {
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 4 } };
      return { status: 200, body: withStaff };
    });

    const thread = await renderThread({ status: "anonymous" });

    // The regular author renders NO badge at all.
    const badges = [...root(thread).querySelectorAll(".ct-badge")];
    expect(badges).toHaveLength(4);
    expect(badges.map((b) => b.className)).toEqual([
      "ct-badge ct-badge--moderator",
      "ct-badge ct-badge--admin",
      "ct-badge ct-badge--super-admin",
      "ct-badge",
    ]);
    // The role rides the BORDER colour — and is ALSO in the text, so colour is
    // never the only channel. It is a visually hidden suffix (an aria-label on
    // a plain span would be ignored: role `generic` cannot be named), with the
    // visible «Ομάδα» first (the label-in-name rule).
    const expectedLabels: Array<string | null> = [null, null, null, null];
    expect(badges.map((b) => b.getAttribute("aria-label"))).toEqual(
      expectedLabels,
    );
    const expectedSuffixes: Array<string | undefined> = [
      `: ${el.account.roleModerator}`,
      `: ${el.account.roleAdmin}`,
      `: ${el.account.roleSuperAdmin}`,
      undefined,
    ];
    expect(badges.map((b) => b.querySelector(".sr-only")?.textContent)).toEqual(
      expectedSuffixes,
    );
    const expectedTitles: Array<string | null> = [
      el.account.roleModerator,
      el.account.roleAdmin,
      el.account.roleSuperAdmin,
      null,
    ];
    expect(badges.map((b) => b.getAttribute("title"))).toEqual(expectedTitles);
    expect(badges.map((b) => b.textContent)).toEqual([
      `${el.comments.staffBadge}: ${el.account.roleModerator}`,
      `${el.comments.staffBadge}: ${el.account.roleAdmin}`,
      `${el.comments.staffBadge}: ${el.account.roleSuperAdmin}`,
      el.comments.staffBadge,
    ]);
  });

  test("dispatches the count-sync event after each count refetch", async () => {
    const details: unknown[] = [];
    const listener = (e: Event) => {
      details.push((e as CustomEvent).detail);
    };
    window.addEventListener(COMMENT_COUNT_CHANGED_EVENT, listener);
    windowCleanups.push(() =>
      window.removeEventListener(COMMENT_COUNT_CHANGED_EVENT, listener),
    );

    let countCalls = 0;
    stubFetch(async (url) => {
      if (url.endsWith("/comments/count")) {
        countCalls += 1;
        return { status: 200, body: { count: countCalls === 1 ? 3 : 1 } };
      }
      return { status: 200, body: LIST_BODY };
    });

    const thread = await renderThread();
    // Mount refetch: count 3.
    await settle();
    // The element stays mounted for the assertion window only — the
    // render itself is exercised (it must have fetched the count).
    void thread;

    expect(details.length).toBeGreaterThanOrEqual(1);
    expect(details[0]).toEqual({
      kind: "blog-posts",
      contentId: "b1",
      count: 3,
    });
  });

  test("a failed load renders the error banner and retry recovers", async () => {
    let calls = 0;
    stubFetch(async (url) => {
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 0 } };
      calls += 1;
      if (calls === 1) {
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
      return { status: 200, body: EMPTY_BODY };
    });

    const thread = await renderThread({ status: "anonymous" });

    expect(root(thread).querySelector("error-banner")).not.toBeNull();

    (root(thread).querySelector(".ct-retry") as HTMLButtonElement).click();
    await settle();
    await thread.updateComplete;

    expect(root(thread).querySelector(".ct-empty")).not.toBeNull();
  });

  test("an empty thread renders the empty state", async () => {
    stubFetch(async (url) => {
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 0 } };
      return { status: 200, body: EMPTY_BODY };
    });

    const thread = await renderThread({ status: "anonymous" });

    expect(text(root(thread), ".ct-empty")).toBe(el.comments.empty);
  });

  // ── Sort tabs ────────────────────────────────────────────────────

  test("sort radios sync to the URL and refetch with the new sort", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 0 } };
      return { status: 200, body: EMPTY_BODY };
    });

    const thread = await renderThread({ status: "anonymous" });

    const newest = root(thread).querySelector(
      'input[value="newest"]',
    ) as HTMLInputElement;
    expect(newest.checked).toBe(false);
    newest.click();
    await settle();
    await thread.updateComplete;

    // pushState carried ?sort=newest; the reload re-reads the URL.
    expect(window.location.search).toBe("?sort=newest");
    const listCalls = urls.filter((u) => !u.endsWith("/count"));
    expect(listCalls[listCalls.length - 1]).toBe(
      "/api/v1/blog-posts/b1/comments?sort=newest&limit=20",
    );
    expect(newest.checked).toBe(true);
  });

  // ── Composer ─────────────────────────────────────────────────────

  test("the authenticated composer posts, then replaceStates the fragment and reloads with focus", async () => {
    setCSRFToken("tok");
    const calls: Array<{ method: string; url: string }> = [];
    stubFetch(async (url, method) => {
      calls.push({
        method,
        url,
      });
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 1 } };
      if (method === "POST") {
        return {
          status: 201,
          headers: {
            Location: "/api/v1/blog-posts/b1/comments?focus=newid",
          },
          body: "",
        };
      }
      if (url.includes("focus=newid")) return { status: 200, body: LIST_BODY };
      return { status: 200, body: EMPTY_BODY };
    });

    const thread = await renderThread();
    const textarea = root(thread).querySelector(
      ".ct-composer textarea",
    ) as HTMLTextAreaElement;
    textarea.value = "Ένα νέο σχόλιο";
    textarea.dispatchEvent(new Event("input"));
    (
      root(thread).querySelector(".ct-composer .ct-submit") as HTMLButtonElement
    ).click();
    await settle();
    await thread.updateComplete;

    const post = calls.find((c) => c.method === "POST");
    expect(post?.url).toBe("/api/v1/blog-posts/b1/comments");
    expect(calls.some((c) => c.url.includes("focus=newid"))).toBe(true);
    // The fragment is the deep-link resource for the new comment.
    expect(window.location.hash).toBe("#comment-newid");
  });

  test("a 422 on create shows the body violation inline", async () => {
    setCSRFToken("tok");
    stubFetch(async (url, method) => {
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 0 } };
      if (method === "POST") {
        return {
          status: 422,
          body: {
            type: "/problems/validation",
            title: "Validation Failed",
            status: 422,
            violations: [{ field: "body", code: "maxLength" }],
          },
        };
      }
      return { status: 200, body: EMPTY_BODY };
    });

    const thread = await renderThread();
    (
      root(thread).querySelector(".ct-composer textarea") as HTMLTextAreaElement
    ).value = "x";
    (
      root(thread).querySelector(".ct-composer textarea") as HTMLTextAreaElement
    ).dispatchEvent(new Event("input"));
    (
      root(thread).querySelector(".ct-composer .ct-submit") as HTMLButtonElement
    ).click();
    await settle();
    await thread.updateComplete;

    expect(text(root(thread), ".ct-composer .ct-form-error")).toBe(
      el.violations.maxLength,
    );
  });

  test("the composer opens with the viewer's byline above the draft; the foot holds only the submit", async () => {
    stubFetch(async (url) => {
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 0 } };
      return { status: 200, body: EMPTY_BODY };
    });

    const thread = await renderThread();
    const composer = root(thread).querySelector(".ct-composer") as HTMLElement;

    // The identity line mirrors a comment's byline: avatar column beside a
    // `.ct-meta` line whose `.ct-name` is the viewer.
    expect(text(composer, ".ct-meta .ct-name")).toBe("Katakuri");
    // The name renders exactly once in the composer (the avatar's initials
    // are a letter, not the name — a hint left beside the button would make
    // this two).
    expect(composer.textContent?.match(/Katakuri/g)?.length).toBe(1);
    // The byline precedes the draft: the name reads as the author, not as a
    // footer under the field.
    const byline = composer.querySelector(".ct-meta") as HTMLElement;
    const draft = composer.querySelector("textarea") as HTMLElement;
    expect(
      byline.compareDocumentPosition(draft) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();

    const foot = composer.querySelector(".ct-composer-foot") as HTMLElement;
    expect(foot.querySelectorAll("button").length).toBe(1);
    expect(foot.querySelector(".ct-hint")).toBeNull();
    expect(foot.textContent?.trim()).toBe(el.comments.submit);
  });

  test("the composer foot pins the submit-only, right-aligned CSS contract", () => {
    // happy-dom resolves no CSS, so the layout contract is asserted on the
    // sheet text: the submit must stay right-aligned under the draft, and
    // the retired hint class must stay out of the sheet. Comment-blind —
    // prose naming `.ct-hint` must not satisfy or fail these pins.
    const css = stripCssComments(threadStyles);
    expect(css).toMatch(
      /\.ct-composer-foot\s*\{[^}]*justify-content:\s*flex-end/,
    );
    expect(css).not.toMatch(/\.ct-hint/);
  });

  // ── Reply ────────────────────────────────────────────────────────

  test("the reply composer opens under a top-level comment and posts the reply", async () => {
    setCSRFToken("tok");
    const calls: Array<{ method: string; url: string }> = [];
    stubFetch(async (url, method) => {
      calls.push({ method, url });
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 2 } };
      if (method === "POST") {
        return {
          status: 201,
          headers: {
            Location: "/api/v1/blog-posts/b1/comments?focus=reply9",
          },
          body: "",
        };
      }
      return { status: 200, body: LIST_BODY };
    });

    const thread = await renderThread();

    // The reply action lives on the FIRST top-level comment.
    const firstComment = root(thread).querySelector(
      ".ct-comment",
    ) as HTMLElement;
    actionByLabel(firstComment, el.comments.reply)!.click();
    await thread.updateComplete;

    const composer = root(thread).querySelector(".ct-reply-composer");
    expect(composer).not.toBeNull();
    const textarea = composer!.querySelector("textarea") as HTMLTextAreaElement;
    textarea.value = "Η απάντησή μου";
    textarea.dispatchEvent(new Event("input"));
    (composer!.querySelector(".ct-submit") as HTMLButtonElement).click();
    await settle();
    await thread.updateComplete;

    const post = calls.find((c) => c.method === "POST");
    expect(post?.url).toBe("/api/v1/blog-posts/b1/comments/c1/replies");
  });

  // ── Hearts ───────────────────────────────────────────────────────

  test("the heart toggles with a confirmed update (PUT + flip + count)", async () => {
    setCSRFToken("tok");
    const calls: string[] = [];
    stubFetch(async (url, method) => {
      calls.push(method);
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 3 } };
      if (method === "PUT") return { status: 204, body: "" };
      return { status: 200, body: LIST_BODY };
    });

    // c1 is authored by u1 — heart it as ANOTHER viewer (u9): self-hearts
    // are forbidden and render no button.
    const thread = await renderThread({ session: { id: "u9" } });

    const heart = root(thread).querySelector(
      ".ct-comment .ct-heart",
    ) as HTMLButtonElement;
    expect(heart.getAttribute("aria-pressed")).toBe("false");
    expect(heart.textContent).toContain("3");
    heart.click();
    timers.fireAll(); // expire the debounce window (the shared debouncer)
    await settle();
    await thread.updateComplete;

    expect(calls).toContain("PUT");
    expect(heart.getAttribute("aria-pressed")).toBe("true");
    expect(heart.textContent).toContain("4");
  });

  test("a failed heart keeps the state and shows the inline error", async () => {
    setCSRFToken("tok");
    stubFetch(async (url, method) => {
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 3 } };
      if (method === "PUT") {
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

    const thread = await renderThread({ session: { id: "u9" } });
    const heart = root(thread).querySelector(
      ".ct-comment .ct-heart",
    ) as HTMLButtonElement;
    heart.click();
    timers.fireAll();
    await settle();
    await thread.updateComplete;

    expect(heart.getAttribute("aria-pressed")).toBe("false");
    expect(text(root(thread), ".ct-form-error")).toBe(
      el.problems["/problems/internal-error"],
    );
  });

  test("an own comment renders NO heart element at all (self-hearts forbidden)", async () => {
    stubFetch(async (url) => {
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 3 } };
      return { status: 200, body: LIST_BODY };
    });

    // The default session user (u1) authored c1 — its action row carries
    // no heart button AND no anonymous plain count; the nested reply by u2
    // keeps its own heart affordance.
    // (Replies render INSIDE the parent's comment container, so the
    // parent's own row is scoped with :scope > .ct-body — the reply's own
    // .ct-body would match a plain descendant selector.)
    const thread = await renderThread();
    const first = root(thread).querySelector(".ct-comment");
    expect(
      first?.querySelector(":scope > .ct-body > .ct-actions .ct-heart"),
    ).toBeNull();
    expect(
      first?.querySelector(":scope > .ct-body > .ct-actions .ct-hearts"),
    ).toBeNull();
    expect(root(thread).querySelector(".ct-reply .ct-heart")).not.toBeNull();

    // The row carries exactly the three actions, each naming itself through
    // `aria-label` and rendering NO text: a stray literal (a doubled closing
    // brace once leaked into the template as a `}` before «Απάντηση») would
    // now show up as text content here.
    const actions = first?.querySelector(":scope > .ct-body > .ct-actions");
    expect(actionLabels(actions as Element)).toEqual([
      el.comments.reply,
      el.comments.edit,
      el.comments.delete,
    ]);
    expect((actions?.textContent ?? "").trim()).toBe("");
    // The icon cannot show a tooltip of its own: the accessible name and the
    // hover title are the SAME catalog wording, so they cannot drift.
    for (const button of actions!.querySelectorAll(".ct-action")) {
      expect(button.getAttribute("title")).toBe(
        button.getAttribute("aria-label"),
      );
    }
  });

  test("a rapid double-click is a net zero — no heart request", async () => {
    setCSRFToken("tok");
    const calls: string[] = [];
    stubFetch(async (url, method) => {
      calls.push(method);
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 3 } };
      return { status: 200, body: LIST_BODY };
    });

    const thread = await renderThread({ session: { id: "u9" } });
    const heart = root(thread).querySelector(
      ".ct-comment .ct-heart",
    ) as HTMLButtonElement;
    heart.click(); // desired = hearted
    heart.click(); // desired = unhearted — net zero
    timers.fireAll();
    await settle();
    await thread.updateComplete;

    expect(calls).not.toContain("PUT");
    expect(calls).not.toContain("DELETE");
    expect(heart.getAttribute("aria-pressed")).toBe("false");
  });

  test("a click-play burst on one comment delivers ONE request (last click wins)", async () => {
    setCSRFToken("tok");
    const calls: string[] = [];
    stubFetch(async (url, method) => {
      calls.push(method);
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 3 } };
      if (method === "PUT") return { status: 204, body: "" };
      return { status: 200, body: LIST_BODY };
    });

    const thread = await renderThread({ session: { id: "u9" } });
    const heart = root(thread).querySelector(
      ".ct-comment .ct-heart",
    ) as HTMLButtonElement;
    // Three rapid clicks: one window, the button stays enabled and busy
    // (aria-busy), and exactly one PUT lands after the window expires.
    heart.click();
    await thread.updateComplete;
    expect(timers.open()).toBe(1);
    expect(heart.disabled).toBe(false);
    expect(heart.getAttribute("aria-busy")).toBe("true");
    heart.click();
    heart.click();
    expect(timers.open()).toBe(1);

    timers.fireAll();
    await settle();
    await thread.updateComplete;

    expect(calls.filter((m) => m === "PUT")).toHaveLength(1);
    expect(heart.getAttribute("aria-pressed")).toBe("true");
    expect(heart.getAttribute("aria-busy")).toBe("false");
  });

  // ── Edit ─────────────────────────────────────────────────────────

  test("edit is author-only and saves via PATCH in place", async () => {
    setCSRFToken("tok");
    const calls: string[] = [];
    stubFetch(async (url, method) => {
      calls.push(method);
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 3 } };
      if (method === "PATCH") {
        return {
          status: 200,
          body: commentItem({
            body: "Ενημερωμένο κείμενο",
            updatedAt: "2026-08-28T12:00:00.000Z",
          }),
        };
      }
      return { status: 200, body: LIST_BODY };
    });

    // u1 authored c1 — the first comment carries an edit action; the
    // second (u3's) does not.
    const thread = await renderThread();
    const comments = [...root(thread).querySelectorAll(".ct-comment")];

    expect(actionLabels(comments[0] as HTMLElement)).toContain(
      el.comments.edit,
    );
    expect(actionLabels(comments[1] as HTMLElement)).not.toContain(
      el.comments.edit,
    );

    actionByLabel(comments[0] as HTMLElement, el.comments.edit)!.click();
    await thread.updateComplete;

    const editor = root(thread).querySelector(".ct-editor");
    const textarea = editor!.querySelector("textarea") as HTMLTextAreaElement;
    textarea.value = "Ενημερωμένο κείμενο";
    textarea.dispatchEvent(new Event("input"));
    (editor!.querySelector(".ct-submit") as HTMLButtonElement).click();
    await settle();
    await thread.updateComplete;

    expect(calls).toContain("PATCH");
    expect(text(root(thread), ".ct-comment .ct-text")).toBe(
      "Ενημερωμένο κείμενο",
    );
    expect(root(thread).querySelector(".ct-editor")).toBeNull();
  });

  // ── Delete ───────────────────────────────────────────────────────

  test("a confirmed author delete removes the comment AND its replies, then refreshes the count", async () => {
    windowCleanups.push(
      stubProperty(
        window,
        "confirm",
        mock(() => true),
      ),
    );
    setCSRFToken("tok");
    const calls: string[] = [];
    const countBodies: number[] = [];
    stubFetch(async (url, method) => {
      calls.push(`${method} ${url}`);
      if (url.endsWith("/comments/count")) {
        countBodies.push(1);
        return {
          status: 200,
          body: { count: countBodies.length === 1 ? 3 : 1 },
        };
      }
      if (method === "DELETE") return { status: 204, body: "" };
      return { status: 200, body: LIST_BODY };
    });

    const thread = await renderThread();
    const firstComment = root(thread).querySelector(
      ".ct-comment",
    ) as HTMLElement;
    actionByLabel(firstComment, el.comments.delete)!.click();
    await settle();
    await thread.updateComplete;

    expect(calls).toContain("DELETE /api/v1/blog-posts/b1/comments/c1");
    // c1 gone — its nested reply went with it (hard cascade).
    expect(root(thread).querySelector("#comment-c1")).toBeNull();
    expect(root(thread).querySelector("#comment-r1")).toBeNull();
    // The count refresh landed (3 → 1).
    expect(text(root(thread), ".ct-title")).toBe("Σχόλια (1)");
  });

  test("a declined delete confirmation sends nothing", async () => {
    windowCleanups.push(
      stubProperty(
        window,
        "confirm",
        mock(() => false),
      ),
    );
    const methods: string[] = [];
    stubFetch(async (_url, method) => {
      methods.push(method);
      if (method === "DELETE") return { status: 204, body: "" };
      return { status: 200, body: LIST_BODY };
    });

    await renderThread();

    expect(methods).not.toContain("DELETE");
  });

  test("the delete icon names its pending state while the request is out", async () => {
    windowCleanups.push(
      stubProperty(
        window,
        "confirm",
        mock(() => true),
      ),
    );
    let resolveDelete: (r: {
      status: number;
      body: unknown;
    }) => void = () => {};
    stubFetch(async (url, method) => {
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 3 } };
      if (method === "DELETE") {
        // Hold the DELETE open so the in-flight state is observable.
        return new Promise((res) => {
          resolveDelete = res;
        });
      }
      return { status: 200, body: LIST_BODY };
    });

    const thread = await renderThread();
    const first = root(thread).querySelector(".ct-comment") as HTMLElement;
    actionByLabel(first, el.comments.delete)!.click();
    await settle();
    await thread.updateComplete;

    // The glyph cannot spell "in flight": the label does, and the button
    // is disabled so a second press cannot re-send the same delete.
    const pending = actionByLabel(
      root(thread).querySelector(".ct-comment") as HTMLElement,
      el.comments.deletePending,
    );
    expect(pending?.disabled).toBe(true);
    expect(pending?.getAttribute("title")).toBe(el.comments.deletePending);

    resolveDelete({ status: 204, body: "" });
    await settle();
    await thread.updateComplete;
    // The row leaves the DOM with its delete (hard cascade in the store),
    // so the pending label cannot linger on a stale button.
    expect(root(thread).querySelector("#comment-c1")).toBeNull();
    expect(root(thread).querySelector("#comment-r1")).toBeNull();
  });

  test("a moderator sees the delete action on others' comments", async () => {
    stubFetch(async (url) => {
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 3 } };
      return { status: 200, body: LIST_BODY };
    });

    const thread = await renderThread({
      session: { id: "u9", role: "moderator" },
    });

    // c1 belongs to u1, c2 to u3 — a moderator (u9) gets delete on both.
    const comments = [...root(thread).querySelectorAll(".ct-comment")];
    for (const c of comments) {
      expect(actionLabels(c as HTMLElement)).toContain(el.comments.delete);
    }
  });

  // ── Load-more ────────────────────────────────────────────────────

  test("load-more renders only on hasNextPage and appends the next page", async () => {
    const calls: Array<{ url: string }> = [];
    stubFetch(async (url) => {
      calls.push({ url });
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 2 } };
      if (url.includes("after=cur1")) {
        return {
          status: 200,
          body: {
            items: [commentItem({ id: "c3", body: "Τρίτο σχόλιο" })],
            pageInfo: { hasNextPage: false, endCursor: null, total: 2 },
          },
        };
      }
      return {
        status: 200,
        body: {
          items: [commentItem()],
          pageInfo: { hasNextPage: true, endCursor: "cur1", total: 30 },
        },
      };
    });

    const thread = await renderThread({ status: "anonymous" });
    const more = root(thread).querySelector(
      ".ct-more:not(.ct-more-replies)",
    ) as HTMLButtonElement;
    expect(more).not.toBeNull();
    // The sort-aware label: under the default top sort the next page is
    // lower-ranked, not older.
    expect(more.textContent?.trim()).toBe(el.comments.loadMoreTop);

    more.click();
    await settle();
    await thread.updateComplete;

    expect(calls.some((c) => c.url.includes("after=cur1"))).toBe(true);
    expect(root(thread).querySelectorAll(".ct-comment").length).toBe(2);
    expect(
      root(thread).querySelector(".ct-more:not(.ct-more-replies)"),
    ).toBeNull();
  });

  test("a top-level item's reply window appends its next reply page", async () => {
    const calls: Array<{ url: string }> = [];
    stubFetch(async (url) => {
      calls.push({ url });
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 6 } };
      if (url.includes("/comments/c1/replies")) {
        return {
          status: 200,
          body: {
            items: [replyItem("r4"), replyItem("r5")],
            pageInfo: { hasNextPage: false, endCursor: null, total: 2 },
          },
        };
      }
      return {
        status: 200,
        body: {
          items: [
            commentItem({
              replies: [replyItem("r1"), replyItem("r2"), replyItem("r3")],
              replyCount: 5,
              repliesEndCursor: "br1.cur",
            }),
          ],
          pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
        },
      };
    });

    const thread = await renderThread({ status: "anonymous" });
    const more = root(thread).querySelector(
      ".ct-more-replies",
    ) as HTMLButtonElement;
    expect(more).not.toBeNull();
    // The total rides the label (the item's window is incomplete).
    expect(more.textContent?.trim()).toBe(`${el.comments.loadMoreReplies} (5)`);
    expect(root(thread).querySelectorAll(".ct-reply").length).toBe(3);

    more.click();
    await thread.updateComplete;
    // While the page is in flight the button shows the shared pending label
    // and cannot double-fire.
    expect(more.textContent?.trim()).toBe(el.comments.loadingMore);
    expect(more.disabled).toBe(true);

    await settle();
    await thread.updateComplete;

    // The append uses the item's opaque continuation, not a client-built
    // cursor.
    expect(
      calls.some((c) =>
        c.url.includes("/comments/c1/replies?limit=20&after=br1.cur"),
      ),
    ).toBe(true);
    expect(root(thread).querySelectorAll(".ct-reply").length).toBe(5);
    // endCursor null ends the window — the button is gone.
    expect(root(thread).querySelector(".ct-more-replies")).toBeNull();
  });

  test("a failed reply append keeps the window and shows the mapped error", async () => {
    stubFetch(async (url) => {
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 6 } };
      if (url.includes("/comments/c1/replies"))
        return { status: 500, body: { type: "/problems/internal-error" } };
      return {
        status: 200,
        body: {
          items: [
            commentItem({
              replies: [replyItem("r1"), replyItem("r2"), replyItem("r3")],
              replyCount: 5,
              repliesEndCursor: "br1.cur",
            }),
          ],
          pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
        },
      };
    });

    const thread = await renderThread({ status: "anonymous" });
    const more = root(thread).querySelector(
      ".ct-more-replies",
    ) as HTMLButtonElement;
    more.click();
    await settle();
    await thread.updateComplete;

    // The failure is per-item and inline: the window is intact, the button
    // can be retried, and the message comes from the catalog mapper.
    expect(root(thread).querySelectorAll(".ct-reply").length).toBe(3);
    expect(text(root(thread), ".ct-form-error")).toBe(
      el.problems["/problems/internal-error"],
    );
    const retry = root(thread).querySelector(
      ".ct-more-replies",
    ) as HTMLButtonElement;
    expect(retry).not.toBeNull();
    expect(retry.disabled).toBe(false);
  });

  // ── Deep link ────────────────────────────────────────────────────

  test("a #comment- fragment becomes the focus param with the chip and highlight", async () => {
    setHappyDOMURL("https://example.com/blog/b1#comment-c2");
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 3 } };
      return { status: 200, body: LIST_BODY };
    });

    const thread = await renderThread({ status: "anonymous" });

    expect(urls[0]).toBe(
      "/api/v1/blog-posts/b1/comments?sort=top&limit=20&focus=c2",
    );

    const target = root(thread).querySelector("#comment-c2");
    expect(target?.classList.contains("is-target")).toBe(true);
    expect(text(target as HTMLElement, ".ct-target-chip")).toBe(
      el.comments.targetChip,
    );
    expect(root(thread).querySelector(".ct-deleted-notice")).toBeNull();
  });

  test("a focus whose target is gone shows the deleted-target notice", async () => {
    setHappyDOMURL("https://example.com/blog/b1#comment-gone");
    stubFetch(async (url) => {
      if (url.endsWith("/comments/count"))
        return { status: 200, body: { count: 3 } };
      // The server's focus fallback: first page without the anchor.
      return { status: 200, body: LIST_BODY };
    });

    const thread = await renderThread({ status: "anonymous" });

    expect(root(thread).querySelector(".ct-target-chip")).toBeNull();
    expect(text(root(thread), ".ct-deleted-notice")).toBe(
      el.comments.deletedNotice,
    );
  });
});
