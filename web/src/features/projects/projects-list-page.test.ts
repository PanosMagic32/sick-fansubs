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
import { el } from "@shared/catalog/el.js";

// Side-effect import registers the custom element; the type import is
// erased at compile time (bun tree-shakes type-only named imports — a
// type-only import of the class would silently skip registration).
import "./projects-list-page.js";
import type { ProjectsListPage } from "./projects-list-page.js";
import type { ContentCard } from "@shared/ui/content-card.js";

const listStyles = stripCssComments(
  await Bun.file(new URL("./projects-list-page.css", import.meta.url)).text(),
);

/**
 * Fetch-level router: the page under test talks to the real transport +
 * real projects-api module; only fetch is stubbed, so the test covers the
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

interface Project {
  id: string;
  title: string;
  description: string;
  slug: string;
  thumbnailUrl: string | null;
  publishedAt: string;
  updatedAt: string;
  commentCount: number;
  favoriteCount: number;
  creator: { id: string; username: string; avatarUrl: string | null } | null;
}

function projects(count: number, offset = 0): Project[] {
  return Array.from({ length: count }, (_, i) => ({
    id: `pr${offset + i + 1}`,
    title: `Τίτλος ${offset + i + 1}`,
    description: `Περιγραφή ${offset + i + 1}`,
    // Slugs are data only — never an address key, never rendered on the
    // public list; the fixture keeps one so the wire shape stays covered.
    slug: `titlos-${offset + i + 1}`,
    thumbnailUrl: "https://example.com/t.jpg",
    publishedAt: "2026-08-15T18:30:00.000Z",
    updatedAt: "2026-08-15T18:30:00.000Z",
    commentCount: 0,
    favoriteCount: 0,
    // pr1 has a creator with an avatar; pr2 a creator without one; the
    // rest none — the three avatar-chip shapes stay covered.
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
  items: Project[],
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

async function renderPage(search = ""): Promise<ProjectsListPage> {
  setHappyDOMURL(`https://example.com/projects${search}`);
  const page = document.createElement("projects-list-page") as ProjectsListPage;
  document.body.appendChild(page);
  await flush(page);
  return page;
}

/** Flush microtasks + a render — mocked fetches resolve across several
 * microtask hops (request → json → state update), so a macrotask hop
 * guarantees the chain has settled before updateComplete is read. */
async function flush(page: ProjectsListPage) {
  await new Promise((r) => setTimeout(r, 0));
  await page.updateComplete;
}

function root(page: ProjectsListPage): HTMLElement {
  return page.shadowRoot! as unknown as HTMLElement;
}

/** The rendered content cards, in document order. */
function cards(page: ProjectsListPage): ContentCard[] {
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
function pagerRoot(page: ProjectsListPage): HTMLElement {
  const pager = root(page).querySelector("pager-nav");
  if (!pager) throw new Error("pager-nav not rendered");
  return pager.shadowRoot as unknown as HTMLElement;
}

/** The pager's chip buttons in DOM order: [Previous, Next]. */
function pagerButtons(page: ProjectsListPage): HTMLButtonElement[] {
  return [...pagerRoot(page).querySelectorAll("button")] as HTMLButtonElement[];
}

/**
 * Read the message inside an <error-banner> host — the banner renders its
 * alert paragraph in its OWN shadow root (same helper shape as the blog
 * tests).
 */
function bannerText(page: ProjectsListPage): string {
  const host = page.shadowRoot?.querySelector("error-banner");
  return host?.shadowRoot?.querySelector(".error")?.textContent?.trim() ?? "";
}

describe("projects-list-page", () => {
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

    const page = document.createElement(
      "projects-list-page",
    ) as ProjectsListPage;
    document.body.appendChild(page);
    await page.updateComplete;

    expect(root(page).querySelector("loading-spinner")).not.toBeNull();

    resolve({ status: 200, body: pageBody(projects(3), false, null) });
    await flush(page);
  });

  test("renders the grid and pages through the URL-carried cursor", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return url.includes("after=")
        ? { status: 200, body: pageBody(projects(2, 10), false, null) }
        : { status: 200, body: pageBody(projects(10), true, "pv1.abc") };
    });

    const page = await renderPage();

    // Grid-only — all 10 first-page items render
    // the shared card in its grid shape, none promoted to a hero. The
    // first page is requested with the explicit limit of 10. The cards
    // sit under the sr-only h1 alone, so they are h2.
    const rendered = cards(page);
    expect(rendered.length).toBe(10);
    expect(rendered.every((card) => card.shape === "grid")).toBe(true);
    expect(root(page).querySelector('content-card[shape="hero"]')).toBeNull();
    expect(rendered[0]?.headingLevel).toBe(2);
    expect(urls[0]).toBe("/api/v1/projects?limit=10");
    // Page 1: Previous disabled (no marker), Next live.
    expect(pagerButtons(page)[0]?.disabled).toBe(true);
    expect(pagerButtons(page)[1]?.disabled).toBe(false);

    // Next: the continuation cursor rides the URL — no load-more.
    pagerButtons(page)[1]!.dispatchEvent(new Event("click", { bubbles: true }));
    await flush(page);

    expect(urls[1]).toBe("/api/v1/projects?limit=10&after=pv1.abc");
    expect(window.location.search).toBe("?limit=10&after=pv1.abc&page=2");
    // Results are REPLACED, never appended (page-based, not load-more).
    expect(cards(page).length).toBe(2);
    // Previous is now enabled (hasNextPage=false disables Next).
    const buttons = pagerButtons(page);
    expect(buttons[0]?.disabled).toBe(false);
    expect(buttons[1]?.disabled).toBe(true);
    // The page-turn chips are arrow-only; the catalog words moved to the
    // accessible name.
    expect(buttons[0]?.getAttribute("aria-label")).toBe(el.ui.prevPage);
  });

  test("mounting directly on a cursor page disables Previous until next is used", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody(projects(2, 10), false, null) };
    });

    const page = await renderPage("?after=pv1.abc");

    // The fetch carries the cursor; Previous stays DISABLED — a
    // shared/deep-linked URL has no sfPager marker (history.back() would
    // leave the app). Next is disabled too (hasNextPage=false).
    expect(urls[0]).toBe("/api/v1/projects?limit=10&after=pv1.abc");
    expect(cards(page).length).toBe(2);
    expect(pagerButtons(page)[0]?.disabled).toBe(true);
    expect(pagerButtons(page)[1]?.disabled).toBe(true);
  });

  test("after a refresh on a cursor page, Previous stays available (the state marker persists)", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return url.includes("after=")
        ? { status: 200, body: pageBody(projects(2, 10), false, null) }
        : { status: 200, body: pageBody(projects(10), true, "pv1.abc") };
    });

    const page = await renderPage();
    pagerButtons(page)[1]!.dispatchEvent(new Event("click", { bubbles: true }));
    await flush(page);
    expect(pagerButtons(page)[0]?.disabled).toBe(false); // previous live
    page.remove();

    // "Refresh": a NEW page instance mounts on the same cursor URL.
    // Session-history state survives a reload, so the sfPager marker does
    // too — Previous must remain enabled.
    const refreshed = await renderPage("?limit=10&after=pv1.abc");
    expect(pagerButtons(refreshed)[0]?.disabled).toBe(false);
    expect(pagerButtons(refreshed)[1]?.disabled).toBe(true);
  });

  test("a stale in-flight response never paints after a newer load", async () => {
    const resolvers: Array<(r: { status: number; body: unknown }) => void> = [];
    stubFetch(
      () =>
        new Promise((resolve) => {
          resolvers.push(resolve);
        }),
    );

    const page = document.createElement(
      "projects-list-page",
    ) as ProjectsListPage;
    document.body.appendChild(page);
    await page.updateComplete;

    // A newer load starts (a navigate-event re-sync on a cursor URL) while
    // the first is still in flight.
    setHappyDOMURL("https://example.com/projects?limit=10&after=pv1.abc");
    window.dispatchEvent(
      new CustomEvent("sf-navigate", { detail: { path: "/projects" } }),
    );
    await page.updateComplete;

    // The newer request resolves first…
    resolvers[1]!({
      status: 200,
      body: pageBody(projects(2, 10), false, null),
    });
    await flush(page);
    expect(cards(page)[0]?.title).toBe("Τίτλος 11");

    // …and the aborted first finally lands with stale data — it must not
    // paint over the newer page.
    resolvers[0]!({ status: 200, body: pageBody(projects(5), false, null) });
    await flush(page);
    expect(cards(page).length).toBe(2);
    expect(cards(page)[0]?.title).toBe("Τίτλος 11");
  });

  test("changing the page size restarts at the first page with the new size", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody(projects(3), false, null) };
    });

    const page = await renderPage();
    const select = pagerRoot(page).querySelector("select") as HTMLSelectElement;
    select.value = "20";
    select.dispatchEvent(new Event("change", { bubbles: true }));
    await flush(page);

    // The size rides the URL; the cursor is dropped (a cursor is only
    // meaningful with the same limit) — the first page reloads at size 20.
    expect(window.location.search).toBe("?limit=20");
    expect(urls[1]).toBe("/api/v1/projects?limit=20");
  });

  test("shows the Greek empty state for an empty collection", async () => {
    stubFetch(async () => ({
      status: 200,
      body: pageBody([], false, null),
    }));

    const page = await renderPage();

    expect(text(root(page), ".empty")).toBe("Δεν υπάρχουν projects ακόμα.");
    expect(root(page).querySelector(".card-grid")).toBeNull();
    // The pager is hidden when there is nothing to paginate.
    expect(root(page).querySelector("pager-nav")).toBeNull();
  });

  test("the sr-only heading keeps the anglicism inside lang=en", async () => {
    stubFetch(async () => ({
      status: 200,
      body: pageBody([], false, null),
    }));

    const page = await renderPage();

    expect(
      root(page).querySelector('h1.sr-only span[lang="en"]')?.textContent,
    ).toBe(el.projects.projectsTitle);
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
      return { status: 200, body: pageBody(projects(2), false, null) };
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
        return { status: 200, body: pageBody(projects(2, 10), false, null) };
      }
      return { status: 200, body: pageBody(projects(10), true, "pv1.abc") };
    });

    const page = await renderPage();
    pagerButtons(page)[1]!.dispatchEvent(new Event("click", { bubbles: true }));
    await flush(page);

    // The failed page turn is the same failure path as any load: error
    // banner + retry — one banner behavior on all pages. The
    // pager is hidden on the error state.
    expect(root(page).querySelector("error-banner")).not.toBeNull();
    expect(root(page).querySelector("pager-nav")).toBeNull();
    // The URL still carries the cursor, so retry reloads the SAME page.
    expect(window.location.search).toBe("?limit=10&after=pv1.abc&page=2");
    root(page).querySelector<HTMLButtonElement>(".retry")!.click();
    await flush(page);
    expect(cards(page).length).toBe(2);
  });

  test("renders the indicator stats on every card (chip, both always)", async () => {
    const items = projects(2);
    items[0]!.commentCount = 3;
    items[0]!.favoriteCount = 0;
    items[1]!.commentCount = 1;
    items[1]!.favoriteCount = 2;
    stubFetch(async () => ({
      status: 200,
      body: pageBody(items, false, null),
    }));

    const page = await renderPage();

    // The host carries both counts (rendered together, zeros included);
    // the chip itself renders in the card's shadow root — one chip per
    // card.
    const rendered = cards(page);
    expect(rendered[0]?.commentCount).toBe(3);
    expect(rendered[0]?.favoriteCount).toBe(0);
    const chips = rendered.map((card) =>
      cardRoot(card).querySelector(".content-stats"),
    );
    expect(chips.length).toBe(2);
    // Zero hearts still render — both indicators always.
    expect(chips[0]?.querySelectorAll(".stat").length).toBe(2);
    expect(
      chips[0]?.querySelector('[aria-label="0 αγαπημένα"]'),
    ).not.toBeNull();
    expect(chips[1]?.querySelector('[aria-label="1 σχόλιο"]')).not.toBeNull();
  });

  test("renders the card fields: description quote, avatar chip, details marker", async () => {
    stubFetch(async () => ({
      status: 200,
      body: pageBody(projects(3), false, null),
    }));

    const page = await renderPage();

    // The page wires the card's inputs; the card renders its own content
    // in its OWN shadow root. Description clamped with the quote icons
    // (spaces from the template); icon-only details marker (two plain
    // chevrons, aria-label keeps the affordance accessible); avatar chip
    // beside the date.
    const rendered = cards(page);
    expect(rendered[0]?.title).toBe("Τίτλος 1");
    expect(rendered[0]?.description).toBe("Περιγραφή 1");
    expect(rendered[0]?.creator?.username).toBe("creator");
    expect(rendered[0]?.srUsername).toBe(true);
    expect(text(cardRoot(rendered[0]!), ".description")).toBe(
      "❝ Περιγραφή 1 ❞",
    );
    const more = cardRoot(rendered[0]!).querySelector(".more");
    expect(more?.getAttribute("aria-label")).toBe("Λεπτομέρειες");
    expect(more?.textContent?.trim()).toBe("❯❯");
    expect(
      cardRoot(rendered[0]!)
        .querySelector(".meta img.avatar")
        ?.getAttribute("src"),
    ).toBe("https://example.com/a.png");
    // The avatar-only byline still names its creator for assistive tech.
    expect(text(cardRoot(rendered[0]!), ".meta .sr-only")).toBe("creator");

    // pr2 falls back to the initial circle (creator without avatar); pr3
    // to the empty neutral circle.
    expect(text(cardRoot(rendered[1]!), ".avatar-initial")).toBe("E");
    expect(
      cardRoot(rendered[2]!).querySelector(".avatar-empty"),
    ).not.toBeNull();
  });

  test("renders a neutral placeholder when the thumbnail is masked to null", async () => {
    const items = projects(1);
    // The scheme guard masks a malformed stored thumbnail as null on the
    // wire — the card must not render a broken img.
    items[0]!.thumbnailUrl = null;
    stubFetch(async () => ({
      status: 200,
      body: pageBody(items, false, null),
    }));

    const page = await renderPage();

    // The host receives the null itself: the attribute binding is removed
    // for a null, which the card reads as "no image" — the 16/9 cell stays
    // as a seam and no <img> renders.
    const [card] = cards(page);
    expect(card?.thumbnailUrl).toBeNull();
    expect(cardRoot(card!).querySelector("img.thumb")).toBeNull();
    expect(cardRoot(card!).querySelector(".thumb-empty")).not.toBeNull();
  });

  test("never renders a subtitle element (the projects table has none)", async () => {
    stubFetch(async () => ({
      status: 200,
      body: pageBody(projects(2), false, null),
    }));

    const page = await renderPage();

    // The blog cards carry a subtitle; here the page never sets the host
    // input and the card renders no subtitle element — the wire contract
    // has no subtitle field at all.
    const [card] = cards(page);
    expect(card?.subtitle).toBe("");
    expect(cardRoot(card!).querySelector(".subtitle")).toBeNull();
  });

  test("cards link to their ID-addressed detail pages", async () => {
    stubFetch(async () => ({
      status: 200,
      body: pageBody(projects(4), false, null),
    }));

    const page = await renderPage();

    // Slugs are data only — links are /projects/{id} exactly like
    // /blog/{id}. The shell intercepts same-origin clicks into SPA
    // navigation; the card's own anchor carries the target inside its
    // shadow root.
    const rendered = cards(page);
    expect(rendered.map((card) => card.href)).toEqual([
      "/projects/pr1",
      "/projects/pr2",
      "/projects/pr3",
      "/projects/pr4",
    ]);
    expect(
      cardRoot(rendered[0]!).querySelector("a.link")?.getAttribute("href"),
    ).toBe("/projects/pr1");
  });

  test("formats dates with the numeric Greek date plus time", async () => {
    stubFetch(async () => ({
      status: 200,
      body: pageBody(projects(2), false, null),
    }));

    const page = await renderPage();

    // Same NUMERIC shape as the blog list and the detail pages; the exact
    // time depends on the test environment's time zone, so the SHAPE is
    // what is pinned.
    const dateText = text(cardRoot(cards(page)[0]!), ".meta .date");
    expect(dateText).toMatch(/^\d{2}\/\d{2}\/\d{4}, \d{2}:\d{2}$/);
    expect(dateText).not.toContain("Αυγούστου");
  });
});
