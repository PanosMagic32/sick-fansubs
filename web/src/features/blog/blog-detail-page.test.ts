import { afterEach, describe, expect, mock, test } from "bun:test";

import {
  mockFetchImpl,
  mockResponse,
  restoreMockFetch,
} from "@shared/api/test-utils.js";
import { el } from "@shared/catalog/el.js";
import { FAVORITE_CHANGED_EVENT } from "@shared/events.js";
import { COMMENT_COUNT_CHANGED_EVENT } from "@shared/events.js";
import { formatDateTimeNumeric } from "@shared/utils/format.js";

// Side-effect import registers the custom element (same pattern as the
// list page tests).
import "./blog-detail-page.js";
import type { BlogDetailPage } from "./blog-detail-page.js";
import type { ContentCard } from "@shared/ui/content-card.js";
import type { BlogPostDetail } from "./data-access/types.js";

/** Fetch-level router: component → real blog-api → real transport → stub.
 * The comments section fetches nothing until
 * its CTA is clicked, so these stubs only ever see the detail request. */
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

function samplePost(overrides: Partial<BlogPostDetail> = {}): BlogPostDetail {
  return {
    id: "p1",
    title: "Τίτλος ανάρτησης",
    subtitle: "Υπότιτλος",
    description: "Περιγραφή ανάρτησης",
    thumbnailUrl: "https://example.com/t.jpg",
    publishedAt: "2026-08-15T18:30:00.000Z",
    updatedAt: "2026-08-15T18:30:00.000Z",
    commentCount: 3,
    favoriteCount: 2,
    creator: { id: "u1", username: "creator", avatarUrl: null },
    updater: null,
    downloads: [
      {
        resolution: "1080p",
        magnetUrl: "magnet:?xt=urn:btih:aaa",
        torrentUrl: "https://example.com/t.torrent",
      },
      { resolution: "2160p", magnetUrl: null, torrentUrl: null },
    ],
    ...overrides,
  };
}

function problemBody(type: string, status: number) {
  return { type, title: "X", status, requestId: "rid" };
}

async function renderPage(postId: string) {
  const page = document.createElement("blog-detail-page") as BlogDetailPage;
  page.postId = postId;
  document.body.appendChild(page);
  await flush(page);
  return page;
}

/** Flush microtasks + a render (same helper contract as the list tests). */
async function flush(page: BlogDetailPage) {
  await new Promise((r) => setTimeout(r, 0));
  await page.updateComplete;
  // The lead block's card renders one binding hop behind the page.
  const host = root(page).querySelector("content-card") as ContentCard | null;
  await host?.updateComplete;
}

function root(page: BlogDetailPage): HTMLElement {
  return page.shadowRoot! as unknown as HTMLElement;
}

/** The detail hero block — the page renders exactly one content card. */
function card(page: BlogDetailPage): ContentCard {
  const host = root(page).querySelector("content-card") as ContentCard | null;
  if (!host) throw new Error("content-card not rendered");
  return host;
}

/** The card's own shadow root — the card renders the lead block there. */
function cardRoot(page: BlogDetailPage): HTMLElement {
  return card(page).shadowRoot! as unknown as HTMLElement;
}

function text(root: HTMLElement, selector: string): string {
  // Whitespace is normalized: formatters/editors may re-layout a template
  // across lines, which changes the raw text nodes around interpolations.
  // The contract under test is the CONTENT (quote glyphs + text), not the
  // template's line layout — collapse every whitespace run to one space.
  return (root.querySelector(selector)?.textContent ?? "")
    .replace(/\s+/g, " ")
    .trim();
}

function bannerText(page: BlogDetailPage): string {
  const host = page.shadowRoot?.querySelector("error-banner");
  return host?.shadowRoot?.querySelector(".error")?.textContent?.trim() ?? "";
}

afterEach(() => {
  document.body.innerHTML = "";
  document.title = "";
  mock.restore();
  restoreMockFetch();
});

describe("blog-detail-page", () => {
  test("renders the indicator stats and syncs them from the two window events", async () => {
    stubFetch(async () => ({ status: 200, body: samplePost() }));

    const page = await renderPage("p1");
    await flush(page);

    const statEls = [...cardRoot(page).querySelectorAll(".detail-stats .stat")];
    expect(statEls.length).toBe(2);
    expect(statEls[0]?.getAttribute("aria-label")).toBe("3 σχόλια");
    expect(statEls[1]?.getAttribute("aria-label")).toBe("2 αγαπημένα");

    // A CONFIRMED heart flip bumps the public count.
    window.dispatchEvent(
      new CustomEvent(FAVORITE_CHANGED_EVENT, {
        detail: { kind: "blog-posts", contentId: "p1", favorited: true },
      }),
    );
    await flush(page);
    expect(
      cardRoot(page).querySelector('.detail-stats [aria-label="3 αγαπημένα"]'),
    ).not.toBeNull();

    // The thread's count refetch (after a click) overwrites the comment count.
    window.dispatchEvent(
      new CustomEvent(COMMENT_COUNT_CHANGED_EVENT, {
        detail: { kind: "blog-posts", contentId: "p1", count: 7 },
      }),
    );
    await flush(page);
    expect(
      cardRoot(page).querySelector('.detail-stats [aria-label="7 σχόλια"]'),
    ).not.toBeNull();

    // Events for OTHER content are ignored.
    window.dispatchEvent(
      new CustomEvent(FAVORITE_CHANGED_EVENT, {
        detail: { kind: "blog-posts", contentId: "other", favorited: true },
      }),
    );
    await flush(page);
    expect(
      cardRoot(page).querySelector('.detail-stats [aria-label="3 αγαπημένα"]'),
    ).not.toBeNull();
  });

  test("shows the loading spinner while the request is in flight", async () => {
    let resolve!: (r: { status: number; body: unknown }) => void;
    stubFetch(
      () =>
        new Promise((r) => {
          resolve = r;
        }),
    );

    const page = document.createElement("blog-detail-page") as BlogDetailPage;
    page.postId = "p1";
    document.body.appendChild(page);
    await page.updateComplete;

    expect(root(page).querySelector("loading-spinner")).not.toBeNull();

    resolve({ status: 200, body: samplePost() });
    await flush(page);
  });

  test("renders the post fields, meta, and downloads from the detail API", async () => {
    const calls: string[] = [];
    stubFetch(async (url) => {
      calls.push(url);
      return { status: 200, body: samplePost() };
    });

    const page = await renderPage("p1");

    expect(calls).toEqual(["/api/v1/blog-posts/p1"]);

    // The lead block is the shared content-card: the page only wires its
    // detail inputs (the card's own suite pins the card's rendering).
    const hero = card(page);
    expect(hero.shape).toBe("detail");
    expect(hero.headingLevel).toBe(1);
    expect(hero.title).toBe("Τίτλος ανάρτησης");
    expect(hero.subtitle).toBe("Υπότιτλος");
    expect(hero.description).toBe("Περιγραφή ανάρτησης");
    expect(hero.thumbnailUrl).toBe("https://example.com/t.jpg");
    expect(hero.publishedAt).toBe("2026-08-15T18:30:00.000Z");
    expect(hero.updatedAt).toBe("2026-08-15T18:30:00.000Z");

    // The card renders the title as the page's h1, the description with the
    // quote-glyph treatment, and the thumbnail image.
    expect(text(cardRoot(page), "h1.title")).toBe("Τίτλος ανάρτησης");
    expect(text(cardRoot(page), ".description")).toBe(
      "❝ Περιγραφή ανάρτησης ❞",
    );
    expect(cardRoot(page).querySelector("img.thumb")?.getAttribute("src")).toBe(
      "https://example.com/t.jpg",
    );

    // Published meta with the creator byline; no updated line (updatedAt
    // is not strictly after publishedAt).
    const lines = [
      ...cardRoot(page).querySelectorAll(".detail-meta .meta-line"),
    ];
    expect(lines.length).toBe(1);
    expect(lines[0]!.textContent).toContain("Δημοσιεύτηκε");
    expect(lines[0]!.textContent).toContain("creator");
    // The date is the NUMERIC Greek form: no month word.
    const published = text(lines[0] as HTMLElement, "time");
    expect(published).toBe(formatDateTimeNumeric("2026-08-15T18:30:00.000Z"));
    expect(published).toContain("/");
    // An EN DASH separates the date from the creator's name, and the name
    // carries no «Από» prefix.
    expect(text(lines[0] as HTMLElement, ".meta-separator")).toBe("–");
    expect(text(lines[0] as HTMLElement, ".meta-byline")).toBe("creator");

    // Downloads: 1080p row has both links; the 2160p row has neither.
    // Plain rows only — the layout-exploration toggle is gone; the section
    // matches the project detail page.
    expect(root(page).querySelector(".layout-toggle")).toBeNull();
    const rows = [...root(page).querySelectorAll(".download-row")];
    expect(rows.length).toBe(2);
    expect(text(rows[0] as HTMLElement, ".download-resolution")).toBe("1080p");
    const links = [...rows[0]!.querySelectorAll("a")];
    expect(links.length).toBe(2);
    expect(links[0]!.getAttribute("href")).toBe("magnet:?xt=urn:btih:aaa");
    expect(links[0]!.textContent?.trim()).toBe("Magnet");
    // Deliberate anglicisms (catalog comment): rendered inside lang="en"
    // so assistive tech pronounces them correctly (same convention as the
    // footer's Online/Offline, shared/AGENTS.md).
    expect(links[0]!.getAttribute("lang")).toBe("en");
    expect(links[1]!.getAttribute("href")).toBe(
      "https://example.com/t.torrent",
    );
    expect(links[1]!.getAttribute("lang")).toBe("en");
    expect(rows[1]!.querySelector("a")).toBeNull();

    // The page refines the shell's base title with the post title.
    expect(document.title).toContain("Τίτλος ανάρτησης");
  });

  test("shows the updated meta only when updatedAt is strictly after publishedAt", async () => {
    stubFetch(async () => ({
      status: 200,
      body: samplePost({
        updatedAt: "2026-08-20T10:00:00.000Z",
        updater: {
          id: "u2",
          username: "editor",
          avatarUrl: "https://example.com/e.png",
        },
      }),
    }));

    const page = await renderPage("p1");

    // The page wires the strict comparison through the card's inputs; the
    // card renders the line.
    const hero = card(page);
    expect(hero.updatedAt).toBe("2026-08-20T10:00:00.000Z");
    expect(hero.updater?.username).toBe("editor");

    const lines = [
      ...cardRoot(page).querySelectorAll(".detail-meta .meta-line"),
    ];
    // The two-line shape: the updater keeps its own line and its own
    // avatar chip; the date form and the date/byline separator differ.
    expect(lines.length).toBe(2);
    const updatedLine = lines[1]!;
    expect(updatedLine.textContent).toContain("Ενημερώθηκε");
    expect(updatedLine.textContent).toContain("editor");
    expect(updatedLine.querySelector("img.avatar")?.getAttribute("src")).toBe(
      "https://example.com/e.png",
    );
    expect(
      [...updatedLine.querySelectorAll("time")].map((t) =>
        t.textContent?.trim(),
      ),
    ).toEqual([formatDateTimeNumeric("2026-08-20T10:00:00.000Z")]);
    // The separator carries no text for assistive tech.
    expect(
      updatedLine.querySelector(".meta-separator")?.getAttribute("aria-hidden"),
    ).toBe("true");
    expect(
      updatedLine.querySelector(".meta-separator")?.textContent?.trim(),
    ).toBe("–");
  });

  test("renders no description block when the description is empty", async () => {
    stubFetch(async () => ({
      status: 200,
      body: samplePost({ description: "" }),
    }));

    const page = await renderPage("p1");

    // The block is conditionally rendered — no lone quote glyph and no
    // floating accent bar.
    expect(card(page).description).toBe("");
    expect(cardRoot(page).querySelector(".description")).toBeNull();
  });

  test("renders no downloads section when the post has no downloads", async () => {
    stubFetch(async () => ({
      status: 200,
      body: samplePost({ downloads: [] }),
    }));

    const page = await renderPage("p1");

    expect(root(page).querySelector(".downloads")).toBeNull();
  });

  test("shows the Greek not-found state for a masked 404", async () => {
    stubFetch(async () => ({
      status: 404,
      body: problemBody("/problems/not-found", 404),
    }));

    const page = await renderPage("unknown");

    expect(text(root(page), ".not-found-title")).toBe("Η ανάρτηση δε βρέθηκε.");
    const back = root(page).querySelector(".back-link");
    expect(back?.getAttribute("href")).toBe("/");
    expect(back?.textContent?.trim()).toBe("Επιστροφή στην αρχική");
  });

  test("shows the mapped error banner and recovers on retry", async () => {
    let calls = 0;
    stubFetch(async () => {
      calls++;
      if (calls === 1) {
        return {
          status: 500,
          body: problemBody("/problems/internal-error", 500),
        };
      }
      return { status: 200, body: samplePost() };
    });

    const page = await renderPage("p1");

    expect(bannerText(page)).toBe("Σφάλμα διακομιστή. Δοκιμάστε αργότερα.");

    root(page).querySelector<HTMLButtonElement>(".retry")!.click();
    await flush(page);

    expect(text(cardRoot(page), "h1.title")).toBe("Τίτλος ανάρτησης");
    expect(root(page).querySelector("error-banner")).toBeNull();
  });

  test("a stale in-flight response never paints after a postId change", async () => {
    const resolvers: Array<(r: { status: number; body: unknown }) => void> = [];
    stubFetch(
      () =>
        new Promise((resolve) => {
          resolvers.push(resolve);
        }),
    );

    const page = await renderPage("p1");
    page.postId = "p2";
    await page.updateComplete;

    // The second request resolves first with the newer post.
    resolvers[1]!({
      status: 200,
      body: samplePost({ id: "p2", title: "Δεύτερη ανάρτηση" }),
    });
    await flush(page);
    expect(text(cardRoot(page), "h1.title")).toBe("Δεύτερη ανάρτηση");

    // The aborted first request finally lands with stale data — the page
    // must ignore it (a stale response never paints).
    resolvers[0]!({
      status: 200,
      body: samplePost({ id: "p1", title: "Πρώτη ανάρτηση" }),
    });
    await flush(page);
    expect(text(cardRoot(page), "h1.title")).toBe("Δεύτερη ανάρτηση");
  });

  test("reloads when postId changes without a full remount", async () => {
    const calls: string[] = [];
    stubFetch(async (url) => {
      calls.push(url);
      return { status: 200, body: samplePost() };
    });

    const page = await renderPage("p1");
    expect(text(cardRoot(page), "h1.title")).toBe("Τίτλος ανάρτησης");

    page.postId = "p2";
    await flush(page);

    // The comments section is click-to-load: the post request is the only
    // one at this point — the section fetches nothing before a click.
    expect(calls).toEqual(["/api/v1/blog-posts/p1", "/api/v1/blog-posts/p2"]);
  });

  test("embeds the click-to-load comments section: the count comes from the item, the thread mounts on click", async () => {
    const calls: string[] = [];
    mockFetchImpl(async (input) => {
      const url = typeof input === "string" ? input : input.toString();
      calls.push(url);
      if (url.endsWith("/comments/count")) {
        return mockResponse({ ok: true, status: 200, jsonBody: { count: 9 } });
      }
      if (url.includes("/comments")) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: {
            items: [],
            pageInfo: { hasNextPage: false, endCursor: null, total: 0 },
          },
        });
      }
      return mockResponse({ ok: true, status: 200, jsonBody: samplePost() });
    });

    const page = await renderPage("p1");
    await flush(page);

    const section = root(page).querySelector<HTMLElement>("comments-section");
    expect(section).not.toBeNull();
    const sectionRoot = section!.shadowRoot!;
    expect(sectionRoot.querySelector(".cs-title")?.textContent?.trim()).toBe(
      "Σχόλια (3)",
    );
    expect(sectionRoot.querySelector("comments-thread")).toBeNull();
    expect(calls).toEqual(["/api/v1/blog-posts/p1"]);

    sectionRoot.querySelector<HTMLButtonElement>(".cs-cta")!.click();
    await flush(page);
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    expect(sectionRoot.querySelector("comments-thread")).not.toBeNull();
    expect(calls).toContain("/api/v1/blog-posts/p1/comments?sort=top&limit=20");
    // The count sync runs through the REAL path: the thread's count refetch
    // (stub returns 9) reaches the detail indicator via the window event — no
    // synthetic dispatch, so a broken producer/listener pairing fails here.
    expect(
      cardRoot(page).querySelector('.detail-stats [aria-label="9 σχόλια"]'),
    ).not.toBeNull();
  });

  test("a staff member sees the edit affordance; anonymous visitors don't", async () => {
    stubFetch(async () => ({ status: 200, body: samplePost() }));

    // Anonymous (no session): no edit link — the client-side gate is
    // visibility only; the server enforces the real authorization.
    const anon = await renderPage("b1");
    expect(root(anon).querySelector(".admin-edit-link")).toBeNull();

    // A moderator: the edit affordance renders beside the heart, pointing at
    // the staff edit form.
    const page = document.createElement("blog-detail-page") as BlogDetailPage;
    page.postId = "b1";
    (page as any).session = {
      state: {
        status: "authenticated",
        message: "",
        user: { id: "u1", username: "Admin", role: "moderator" },
      },
      revalidate: mock(async () => {}),
    };
    document.body.appendChild(page);
    await flush(page);

    const link = root(page).querySelector(".admin-edit-link");
    expect(link).not.toBeNull();
    expect(link?.getAttribute("href")).toBe("/admin/blog/p1");
    // Icon-only: the pencil is the visual, the accessible name AND
    // the hover title carry `el.admin.edit` — the projects mirror is pinned
    // the same way.
    expect(link?.textContent?.trim()).toBe("");
    expect(link?.querySelector("svg")).not.toBeNull();
    expect(link?.getAttribute("aria-label")).toBe(el.admin.edit);
    expect(link?.getAttribute("title")).toBe(el.admin.edit);
  });
});
