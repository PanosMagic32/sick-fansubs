import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";

import {
  mockFetchImpl,
  mockResponse,
  resetHappyDOMURL,
  restoreHistoryLocationSync,
  restoreMockFetch,
  setHappyDOMURL,
  shimHistoryLocationSync,
  stripCssComments,
} from "@shared/api/test-utils.js";
import { NAVIGATE_EVENT } from "@core/router.js";
import { el, pagerPageOf } from "@shared/catalog/el.js";

// Side-effect import registers the custom element; the type import is
// erased at compile time (bun tree-shakes type-only named imports — a
// type-only import of the class would silently skip registration).
import "./blog-list-page.js";
import type { BlogListPage } from "./blog-list-page.js";
import type { ContentCard } from "@shared/ui/content-card.js";

const listStyles = stripCssComments(
  await Bun.file(new URL("./blog-list-page.css", import.meta.url)).text(),
);
const statsStyles = stripCssComments(
  await Bun.file(
    new URL("../../shared/ui/content-stats.css", import.meta.url),
  ).text(),
);

/**
 * Fetch-level router: the page under test talks to the real transport +
 * real blog-api module; only fetch is stubbed, so the test covers the
 * whole client stack (component → endpoint → transport → fetch).
 */
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

interface Post {
  id: string;
  title: string;
  subtitle: string;
  description: string;
  thumbnailUrl: string;
  publishedAt: string;
  updatedAt: string;
  commentCount: number;
  favoriteCount: number;
  creator: { id: string; username: string; avatarUrl: string | null } | null;
}

function posts(count: number, offset = 0): Post[] {
  return Array.from({ length: count }, (_, i) => ({
    id: `p${offset + i + 1}`,
    title: `Τίτλος ${offset + i + 1}`,
    // p3 keeps the empty-subtitle case visible in tests.
    subtitle: offset + i + 1 === 3 ? "" : `Υπότιτλος ${offset + i + 1}`,
    description: `Περιγραφή ${offset + i + 1}`,
    thumbnailUrl: "https://example.com/t.jpg",
    publishedAt: "2026-08-15T18:30:00.000Z",
    updatedAt: "2026-08-15T18:30:00.000Z",
    // Indicator counts: p1 carries comments AND favorites (the hero
    // case); the rest carry zero favorites (the zeros-render case).
    commentCount: offset + i + 1,
    favoriteCount: offset + i + 1 === 1 ? 2 : 0,
    // p1 has a creator with an avatar; p2 a creator without one; the rest
    // none — the three avatar-chip shapes stay covered.
    creator:
      offset + i + 1 === 1
        ? {
            id: "u1",
            username: "creator",
            avatarUrl: "https://example.com/a.png",
          }
        : offset + i + 1 === 2
          ? { id: "u2", username: "editor", avatarUrl: null }
          : null,
  }));
}

function pageBody(
  items: Post[],
  hasNextPage: boolean,
  endCursor: string | null,
  total = items.length,
) {
  return { items, pageInfo: { hasNextPage, endCursor, total } };
}

function problemBody(type: string, status: number) {
  return { type, title: "X", status, requestId: "rid" };
}

beforeEach(() => {
  shimHistoryLocationSync();
  // A fresh browser navigation has no history state — the sfPager marker
  // must NOT leak between tests (happy-dom shares the history object).
  window.history.replaceState(null, "", window.location.href);
});

afterEach(() => {
  restoreHistoryLocationSync();
  resetHappyDOMURL();
  document.body.innerHTML = "";
  mock.restore();
  restoreMockFetch();
});

async function renderPage(search = ""): Promise<BlogListPage> {
  setHappyDOMURL(`https://example.com/${search}`);
  const page = document.createElement("blog-list-page") as BlogListPage;
  document.body.appendChild(page);
  await flush(page);
  return page;
}

/** Flush microtasks + a render — mocked fetches resolve across several
 * microtask hops (request → json → state update), so a macrotask hop
 * guarantees the chain has settled before updateComplete is read. */
async function flush(page: BlogListPage) {
  await new Promise((r) => setTimeout(r, 0));
  await page.updateComplete;
}

function root(page: BlogListPage): HTMLElement {
  return page.shadowRoot! as unknown as HTMLElement;
}

/** The rendered content cards, in document order (hero first on page 1). */
function cards(page: BlogListPage): ContentCard[] {
  return [...root(page).querySelectorAll("content-card")] as ContentCard[];
}

/** A card's own shadow root — the card renders its content there. */
function cardRoot(card: ContentCard): HTMLElement {
  return card.shadowRoot! as unknown as HTMLElement;
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

/** The shared pager-nav's shadow root (rendered by the success state). */
function pagerRoot(page: BlogListPage): HTMLElement {
  const pager = root(page).querySelector("pager-nav");
  if (!pager) throw new Error("pager-nav not rendered");
  return pager.shadowRoot as unknown as HTMLElement;
}

/** The pager's chip buttons in DOM order: [Previous, Next]. */
function pagerButtons(page: BlogListPage): HTMLButtonElement[] {
  return [...pagerRoot(page).querySelectorAll("button")] as HTMLButtonElement[];
}

/**
 * Read the message inside an <error-banner> host — the banner renders its
 * alert paragraph in its OWN shadow root (same helper shape as the
 * account-page tests).
 */
function bannerText(page: BlogListPage): string {
  const host = page.shadowRoot?.querySelector("error-banner");
  return host?.shadowRoot?.querySelector(".error")?.textContent?.trim() ?? "";
}

describe("blog-list-page", () => {
  test("grid breakpoints follow the header tiers (CSS contract)", () => {
    // happy-dom cannot evaluate media queries — pin the source: 2 columns
    // at the tablet threshold (≤1150px), 1 at mobile (≤700px), aligned
    // with the header band/dropdown breakpoints. Each media band is
    // extracted whole first, then the grid rule inside it.
    const tablet =
      listStyles.match(/@media \(max-width: 1150px\) \{([\s\S]*?)\n\}/s)?.[1] ??
      "";
    expect(tablet.match(/\.card-grid \{([^}]*)\}/)?.[1] ?? "").toContain(
      "repeat(2, 1fr)",
    );
    const phone =
      listStyles.match(/@media \(max-width: 700px\) \{([\s\S]*?)\n\}/s)?.[1] ??
      "";
    expect(phone.match(/\.card-grid \{([^}]*)\}/)?.[1] ?? "").toContain(
      "grid-template-columns: 1fr",
    );
  });

  test("shows the loading spinner while the first page is in flight", async () => {
    let resolve!: (r: { status: number; body: unknown }) => void;
    stubFetch(
      () =>
        new Promise((r) => {
          resolve = r;
        }),
    );

    const page = document.createElement("blog-list-page") as BlogListPage;
    document.body.appendChild(page);
    await page.updateComplete;

    expect(root(page).querySelector("loading-spinner")).not.toBeNull();

    resolve({ status: 200, body: pageBody(posts(3), false, null) });
    await flush(page);
  });

  test("page 1 renders the hero + grid; Next pages through the URL-carried cursor", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return url.includes("after=")
        ? { status: 200, body: pageBody(posts(2, 10), false, null, 12) }
        : { status: 200, body: pageBody(posts(10), true, "v1.abc", 12) };
    });

    const page = await renderPage();

    // Featured hero = newest post; grid = the rest (9 cards). The first
    // page is requested with the explicit limit of 10. The page only
    // wires the card inputs — the card's own suite pins its rendering.
    const [hero, ...grid] = cards(page);
    expect(hero?.shape).toBe("hero");
    expect(hero?.title).toBe("Τίτλος 1");
    expect(hero?.badge).toBe(el.blog.latestBadge);
    expect(hero?.href).toBe("/blog/p1");
    expect(grid.length).toBe(9);
    expect(grid[0]?.shape).toBe("grid");
    // Heading order: the grid sits under the hero's h2 on page 1 (h3), and
    // under the sr-only h1 alone on a cursor page (h2 — pinned below).
    expect(grid[0]?.headingLevel).toBe(3);
    expect(urls[0]).toBe("/api/v1/blog-posts?limit=10");
    // Page 1: Previous disabled (no marker), Next live.
    expect(pagerButtons(page)[0]?.disabled).toBe(true);
    expect(pagerButtons(page)[1]?.disabled).toBe(false);

    // Next: the continuation cursor rides the URL — no load-more.
    pagerButtons(page)[1]!.dispatchEvent(new Event("click", { bubbles: true }));
    await flush(page);

    expect(urls[1]).toBe("/api/v1/blog-posts?limit=10&after=v1.abc");
    expect(window.location.search).toBe("?limit=10&after=v1.abc&page=2");
    // Page 2: NO hero — the grid renders every item, and the results are
    // REPLACED, never appended.
    const pageTwo = cards(page);
    expect(pageTwo.length).toBe(2);
    expect(pageTwo.every((card) => card.shape === "grid")).toBe(true);
    expect(pageTwo[0]?.headingLevel).toBe(2);
    // The position line reads the URL's ordinal and the loaded row total.
    expect(
      pagerRoot(page).querySelector(".page-status")?.textContent?.trim(),
    ).toBe(pagerPageOf(2, 2));
    // Previous is now enabled (hasNextPage=false disables Next).
    const buttons = pagerButtons(page);
    expect(buttons[0]?.disabled).toBe(false);
    expect(buttons[1]?.disabled).toBe(true);
    // The page-turn chips are arrow-only; the catalog words moved to the
    // accessible name.
    expect(buttons[0]?.getAttribute("aria-label")).toBe(el.ui.prevPage);
  });

  test("mounting directly on a cursor page renders grid-only and hides Previous", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody(posts(2, 10), false, null) };
    });

    const page = await renderPage("?after=v1.abc");

    // The fetch carries the cursor; the hero is a page-1 affordance only.
    expect(urls[0]).toBe("/api/v1/blog-posts?limit=10&after=v1.abc");
    expect(cards(page).length).toBe(2);
    expect(cards(page).every((card) => card.shape === "grid")).toBe(true);
    // Previous must stay DISABLED: a shared/deep-linked URL has no sfPager
    // marker (history.back() would leave the app — same contract as
    // search). Next is disabled too (hasNextPage=false).
    expect(pagerButtons(page)[0]?.disabled).toBe(true);
    expect(pagerButtons(page)[1]?.disabled).toBe(true);
  });

  test("after a refresh on a cursor page, Previous stays available (the state marker persists)", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return url.includes("after=")
        ? { status: 200, body: pageBody(posts(2, 10), false, null) }
        : { status: 200, body: pageBody(posts(10), true, "v1.abc") };
    });

    const page = await renderPage();
    pagerButtons(page)[1]!.dispatchEvent(new Event("click", { bubbles: true }));
    await flush(page);
    expect(pagerButtons(page)[0]?.disabled).toBe(false); // previous live
    page.remove();

    // "Refresh": a NEW page instance mounts on the same cursor URL.
    // Session-history state survives a reload, so the sfPager marker does
    // too — Previous must remain enabled.
    const refreshed = await renderPage("?limit=10&after=v1.abc");
    expect(cards(refreshed).every((card) => card.shape === "grid")).toBe(true);
    expect(pagerButtons(refreshed)[0]?.disabled).toBe(false);
    expect(pagerButtons(refreshed)[1]?.disabled).toBe(true);
  });

  test("changing the page size restarts at the first page with the new size", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody(posts(3), false, null) };
    });

    const page = await renderPage();
    const select = pagerRoot(page).querySelector("select") as HTMLSelectElement;
    select.value = "20";
    select.dispatchEvent(new Event("change", { bubbles: true }));
    await flush(page);

    // The size rides the URL; the cursor is dropped (a cursor is only
    // meaningful with the same limit) — the first page reloads at size 20.
    expect(window.location.search).toBe("?limit=20");
    expect(urls[1]).toBe("/api/v1/blog-posts?limit=20");
  });

  test("back to the first page re-renders the hero (popstate re-sync)", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return url.includes("after=")
        ? { status: 200, body: pageBody(posts(2, 10), false, null) }
        : { status: 200, body: pageBody(posts(10), true, "v1.abc") };
    });

    const page = await renderPage("?after=v1.abc");
    expect(cards(page).every((card) => card.shape === "grid")).toBe(true);

    // Browser back to the bare home URL: popstate re-syncs from the
    // restored URL — the hero returns with the newest post.
    setHappyDOMURL("https://example.com/");
    window.dispatchEvent(new PopStateEvent("popstate"));
    await flush(page);

    expect(urls.at(-1)).toBe("/api/v1/blog-posts?limit=10");
    const [heroPart, ...gridPart] = cards(page);
    expect(heroPart?.shape).toBe("hero");
    expect(heroPart?.title).toBe("Τίτλος 1");
    expect(gridPart.length).toBe(9);
  });

  test("an SPA navigation landing on / re-syncs from the URL", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody(posts(2, 10), false, null) };
    });

    const page = await renderPage("?after=v1.abc");
    expect(cards(page).every((card) => card.shape === "grid")).toBe(true);

    // Production order: the shell pushes the URL first, then the event
    // fires (same flow the search-page tests pin).
    window.history.pushState({}, "", "/");
    window.dispatchEvent(
      new CustomEvent(NAVIGATE_EVENT, { detail: { path: "/" } }),
    );
    await flush(page);

    expect(urls.at(-1)).toBe("/api/v1/blog-posts?limit=10");
    expect(cards(page)[0]?.shape).toBe("hero");
  });

  test("shows the Greek empty state for an empty collection", async () => {
    stubFetch(async () => ({
      status: 200,
      body: pageBody([], false, null),
    }));

    const page = await renderPage();

    expect(text(root(page), ".empty")).toBe("Δεν υπάρχουν αναρτήσεις ακόμα.");
    expect(cards(page).length).toBe(0);
    expect(root(page).querySelector(".card-grid")).toBeNull();
    // The pager is hidden when there is nothing to paginate.
    expect(root(page).querySelector("pager-nav")).toBeNull();
    // The reserved-footprint wrapper keeps the chrome above the state from
    // resizing between loading, empty, and content.
    expect(
      root(page).querySelector(".empty")?.closest(".state-block"),
    ).not.toBeNull();
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
      return { status: 200, body: pageBody(posts(2), false, null) };
    });

    const page = await renderPage();

    // The known problem type maps to its catalog copy (no raw
    // machine identifiers in the UI).
    expect(bannerText(page)).toBe("Σφάλμα διακομιστή. Δοκιμάστε αργότερα.");

    root(page).querySelector<HTMLButtonElement>(".retry")!.click();
    await flush(page);

    expect(cards(page)[0]?.title).toBe("Τίτλος 1");
    expect(root(page).querySelector("error-banner")).toBeNull();
  });

  test("a failing page-2 load renders the SAME error state with retry", async () => {
    let calls = 0;
    stubFetch(async (url) => {
      calls++;
      if (url.includes("after=")) {
        // First page-2 attempt fails; the retry succeeds.
        if (calls === 2) {
          return {
            status: 500,
            body: problemBody("/problems/internal-error", 500),
          };
        }
        return { status: 200, body: pageBody(posts(2, 10), false, null) };
      }
      return { status: 200, body: pageBody(posts(10), true, "v1.abc") };
    });

    const page = await renderPage();
    pagerButtons(page)[1]!.dispatchEvent(new Event("click", { bubbles: true }));
    await flush(page);

    // The failed page turn is the same failure path as any load: error
    // banner + retry — one banner behavior on all pages. The pager is
    // hidden on the error state.
    expect(root(page).querySelector("error-banner")).not.toBeNull();
    expect(root(page).querySelector("pager-nav")).toBeNull();
    // The URL still carries the cursor, so retry reloads the SAME page.
    expect(window.location.search).toBe("?limit=10&after=v1.abc&page=2");
    root(page).querySelector<HTMLButtonElement>(".retry")!.click();
    await flush(page);
    expect(cards(page).length).toBe(2);
    expect(cards(page).every((card) => card.shape === "grid")).toBe(true);
  });

  test("renders the indicator stats: one chip, both indicators always (even zeros)", async () => {
    stubFetch(async () => ({
      status: 200,
      body: pageBody(posts(4), false, null),
    }));

    const page = await renderPage();

    // Hero = p1: 1 comment, 2 favorites — both indicators with the full
    // Greek phrase on each aria-label (role="img" pattern).
    const [hero] = cards(page);
    const heroStats = cardRoot(hero!).querySelector(".content-stats");
    const heroStatsItems = heroStats?.querySelectorAll(".stat");
    expect(heroStatsItems?.length).toBe(2);
    expect(heroStatsItems?.[0]?.getAttribute("aria-label")).toBe("1 σχόλιο");
    expect(heroStatsItems?.[1]?.getAttribute("aria-label")).toBe("2 αγαπημένα");
    // Both icons are SVGs from the shared set — same 1em box.
    expect(heroStats?.querySelectorAll(".stat-icon svg").length).toBe(2);

    // Grid card p3: 3 comments, 0 favorites — the heart STILL renders
    // (a missing heart reads as a missing feature).
    const gridStats = cards(page)
      .slice(1)
      .map((card) => cardRoot(card).querySelector(".content-stats"));
    const p3Stats = gridStats.find(
      (card) => card?.querySelector('[aria-label="3 σχόλια"]') !== null,
    );
    expect(p3Stats?.querySelectorAll(".stat").length).toBe(2);
    expect(p3Stats?.querySelector('[aria-label="0 αγαπημένα"]')).not.toBeNull();
  });

  test("the indicator chip is a bordered pill with a vertical divider (CSS contract)", () => {
    // happy-dom cannot evaluate the visual chip — pin the source: the
    // pill geometry, the divider as the second stat's border-left, and
    // the fit-content width that stops column-flex parents (the account
    // favorites card) from stretching the chip to full width.
    expect(statsStyles).toMatch(/border-radius: 999px/);
    expect(statsStyles).toMatch(/width: fit-content/);
    // The rule body is bounded — the scan cannot reach into a later rule.
    const divider =
      statsStyles.match(/\.stat \+ \.stat \{([^}]*)\}/)?.[1] ?? "";
    expect(divider).toContain("border-left: 1px solid");
  });

  test("renders the styling-pass card fields: description, avatar chip, details marker", async () => {
    stubFetch(async () => ({
      status: 200,
      body: pageBody(posts(4), false, null),
    }));

    const page = await renderPage();

    // Hero (owner 1.3): description below the subtitle wrapped in the
    // quote icons (spaces from the template), icon-only details marker
    // (two plain chevrons, aria-label keeps the affordance accessible),
    // avatar chip beside the date — the hero card's own shadow root.
    const [hero, p2, p3] = cards(page);
    expect(text(cardRoot(hero!), ".description")).toBe("❝ Περιγραφή 1 ❞");
    const heroMore = cardRoot(hero!).querySelector(".more");
    expect(heroMore?.getAttribute("aria-label")).toBe(el.ui.details);
    expect(heroMore?.textContent?.trim()).toBe("❯❯");
    expect(
      cardRoot(hero!).querySelector(".meta img.avatar")?.getAttribute("src"),
    ).toBe("https://example.com/a.png");

    // Grid cards: description (quote icons) + marker; p2 falls back to the
    // initial circle (creator without avatar); p3 to the empty neutral
    // circle.
    expect(text(cardRoot(p2!), ".description")).toBe("❝ Περιγραφή 2 ❞");
    expect(cardRoot(p2!).querySelector(".more")).not.toBeNull();
    expect(text(cardRoot(p2!), ".avatar-initial")).toBe("E");
    expect(cardRoot(p3!).querySelector(".avatar-empty")).not.toBeNull();
  });

  test("hero never renders an edited line", async () => {
    const edited = posts(2);
    edited[0]!.updatedAt = "2026-08-20T10:00:00.000Z";
    stubFetch(async () => ({
      status: 200,
      body: pageBody(edited, false, null),
    }));

    const page = await renderPage();

    // Even with a strictly-later updatedAt, the hero renders no edited
    // line — the hero shape has no byline block at all.
    const [hero] = cards(page);
    expect(cardRoot(hero!).querySelector(".meta-line")).toBeNull();
    expect(cardRoot(hero!).querySelectorAll(".meta .date").length).toBe(1);
  });

  test("hero and cards link to their detail pages", async () => {
    stubFetch(async () => ({
      status: 200,
      body: pageBody(posts(4), false, null),
    }));

    const page = await renderPage();

    // The whole card is a block-level link to /blog/{id} — the shell
    // intercepts same-origin clicks into SPA navigation. The hero and grid
    // cards differ in shape only.
    const [hero, ...grid] = cards(page);
    expect(hero?.href).toBe("/blog/p1");
    expect(grid.map((card) => card.href)).toEqual([
      "/blog/p2",
      "/blog/p3",
      "/blog/p4",
    ]);
    // The card's own anchor carries the target inside its shadow root.
    expect(
      cardRoot(grid[0]!).querySelector("a.link")?.getAttribute("href"),
    ).toBe("/blog/p2");
  });

  test("formats dates with the numeric Greek date plus time", async () => {
    stubFetch(async () => ({
      status: 200,
      body: pageBody(posts(2), false, null),
    }));

    const page = await renderPage();

    // The list view carries the same NUMERIC form as the detail pages —
    // slash date + 24-hour clock, no month word (a long "Αυγούστου" is what
    // crowded a card row). The exact time depends on the test environment's
    // time zone, so the SHAPE is what is pinned.
    const dateText = text(cardRoot(cards(page)[0]!), ".meta .date");
    expect(dateText).toMatch(/^\d{2}\/\d{2}\/\d{4}, \d{2}:\d{2}$/);
    expect(dateText).not.toContain("Αυγούστου");
  });
});
