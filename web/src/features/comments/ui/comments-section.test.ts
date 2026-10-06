/**
 * Comments section tests — the click-to-load wrapper: the collapsed
 * indicator renders from the
 * detail item's count with NO request, the CTA expands and mounts the
 * thread (its list+count reads fire only then), the section takes focus,
 * and a `#comment-<id>` fragment opens the thread on load and on a later
 * popstate.
 */

import { afterEach, beforeEach, describe, expect, test } from "bun:test";

import { el } from "@shared/catalog/el.js";
import {
  mockFetchImpl,
  mockResponse,
  resetHappyDOMURL,
  restoreHistoryLocationSync,
  restoreMockFetch,
  setHappyDOMURL,
  shimHistoryLocationSync,
} from "@shared/api/test-utils.js";

import { navigate } from "@core/router.js";

import "./comments-section.js";
import type { CommentsSection } from "./comments-section.js";

const EMPTY_BODY = {
  items: [],
  pageInfo: { hasNextPage: false, endCursor: null, total: 0 },
};

function stubFetch(
  handler: (url: string) => Promise<{ status: number; body: unknown }>,
) {
  mockFetchImpl(async (input) => {
    const url = typeof input === "string" ? input : input.toString();
    const res = await handler(url);
    return mockResponse({
      ok: res.status < 400,
      status: res.status,
      headers:
        res.status >= 400 ? { "Content-Type": "application/problem+json" } : {},
      jsonBody: res.body,
    });
  });
}

function root(section: CommentsSection): HTMLElement {
  return section.shadowRoot! as unknown as HTMLElement;
}

function text(section: CommentsSection, selector: string): string {
  return (root(section).querySelector(selector)?.textContent ?? "")
    .replace(/\s+/g, " ")
    .trim();
}

function thread(section: CommentsSection): HTMLElement | null {
  return root(section).querySelector("comments-thread");
}

async function settle(): Promise<void> {
  await new Promise((r) => setTimeout(r, 10));
}

async function renderSection(
  overrides: { count?: number; contentId?: string } = {},
): Promise<CommentsSection> {
  const section = document.createElement("comments-section") as CommentsSection;
  section.kind = "blog-posts";
  section.contentId = overrides.contentId ?? "b1";
  section.count = overrides.count ?? 0;
  document.body.appendChild(section);
  await section.updateComplete;
  await settle();
  await section.updateComplete;
  return section;
}

/** One stub for the thread's two reads — the list page and the count. */
function stubThreadReads(calls: string[]) {
  stubFetch(async (url) => {
    calls.push(url);
    if (url.endsWith("/comments/count"))
      return { status: 200, body: { count: 3 } };
    return { status: 200, body: EMPTY_BODY };
  });
}

beforeEach(() => {
  shimHistoryLocationSync();
  setHappyDOMURL("https://example.com/blog/b1");
  window.history.replaceState(null, "", window.location.href);
  document.body.innerHTML = "";
});

afterEach(() => {
  restoreHistoryLocationSync();
  resetHappyDOMURL();
  restoreMockFetch();
  document.body.innerHTML = "";
});

describe("comments-section", () => {
  test("collapsed: renders the count from the detail item and issues NO request", async () => {
    const calls: string[] = [];
    stubThreadReads(calls);

    const section = await renderSection({ count: 3 });

    expect(text(section, ".cs-title")).toBe("Σχόλια (3)");
    expect(text(section, ".cs-cta")).toBe(el.comments.sectionCta);
    expect(text(section, ".cs-hint")).toBe("");
    expect(thread(section)).toBeNull();
    // The section owns the landmark and is the focus target — without the
    // label the post-click focus move would announce nothing.
    expect(root(section).querySelector(".cs")?.getAttribute("aria-label")).toBe(
      el.comments.title,
    );
    expect(root(section).querySelector(".cs")?.getAttribute("tabindex")).toBe(
      "-1",
    );
    expect(calls).toEqual([]);
  });

  test("collapsed at zero: the heading drops the count and the zero-state hint shows", async () => {
    const section = await renderSection({ count: 0 });

    expect(text(section, ".cs-title")).toBe("Σχόλια");
    expect(text(section, ".cs-hint")).toBe(el.comments.empty);
    expect(text(section, ".cs-cta")).toBe(el.comments.sectionCta);
    expect(thread(section)).toBeNull();
  });

  test("the CTA expands: the thread mounts, fetches its list + count, and takes focus", async () => {
    const calls: string[] = [];
    stubThreadReads(calls);

    const section = await renderSection({ count: 3 });
    root(section).querySelector<HTMLButtonElement>(".cs-cta")!.click();
    await section.updateComplete;
    await settle();
    await section.updateComplete;

    expect(thread(section)).not.toBeNull();
    expect(calls).toContain("/api/v1/blog-posts/b1/comments?sort=top&limit=20");
    expect(calls.some((u) => u.endsWith("/comments/count"))).toBe(true);
    // The button is gone — focus moved to the section so the expansion is
    // keyboard-followable.
    expect(root(section).querySelector(".cs-cta")).toBeNull();
    expect(section.shadowRoot!.activeElement).toBe(
      root(section).querySelector(".cs"),
    );
  });

  test("a #comment- fragment opens the thread on load without a click", async () => {
    const calls: string[] = [];
    stubThreadReads(calls);
    setHappyDOMURL("https://example.com/blog/b1#comment-c1");
    window.history.replaceState(null, "", window.location.href);

    const section = await renderSection({ count: 3 });

    expect(thread(section)).not.toBeNull();
    expect(calls.some((u) => u.includes("/comments?sort=top&limit=20"))).toBe(
      true,
    );
  });

  test("a popstate that adds the fragment expands a collapsed section", async () => {
    const calls: string[] = [];
    stubThreadReads(calls);

    const section = await renderSection({ count: 3 });
    expect(thread(section)).toBeNull();

    setHappyDOMURL("https://example.com/blog/b1#comment-c1");
    window.dispatchEvent(new Event("popstate"));
    await section.updateComplete;
    await settle();
    await section.updateComplete;

    expect(thread(section)).not.toBeNull();
  });

  test("an in-app navigate carrying the fragment expands a collapsed section", async () => {
    const calls: string[] = [];
    stubThreadReads(calls);

    const section = await renderSection({ count: 3 });
    expect(thread(section)).toBeNull();

    // The real SPA helper, so the event carries what every in-app jump
    // carries (`router.ts` dispatches a CustomEvent with `detail.path`; a bare
    // Event leaves app-shell's listener without its detail).
    navigate("/blog/b1#comment-c1");
    await section.updateComplete;
    await settle();
    await section.updateComplete;

    expect(thread(section)).not.toBeNull();
    expect(calls.some((u) => u.includes("/comments?sort=top&limit=20"))).toBe(
      true,
    );
  });

  test("a popstate without a fragment leaves the section collapsed", async () => {
    const calls: string[] = [];
    stubThreadReads(calls);

    const section = await renderSection({ count: 3 });
    window.dispatchEvent(new Event("popstate"));
    await section.updateComplete;

    expect(thread(section)).toBeNull();
    // The section owns the landmark and is the focus target — without the
    // label the post-click focus move would announce nothing.
    expect(root(section).querySelector(".cs")?.getAttribute("aria-label")).toBe(
      el.comments.title,
    );
    expect(root(section).querySelector(".cs")?.getAttribute("tabindex")).toBe(
      "-1",
    );
    expect(calls).toEqual([]);
  });
});
