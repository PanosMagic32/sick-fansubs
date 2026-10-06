import { afterEach, beforeEach, describe, expect, test } from "bun:test";

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
import { el } from "@shared/catalog/el.js";

// Side-effect import registers the custom element; the type import is
// erased at compile time.
import "./search-page.js";
import type { SearchPage } from "./search-page.js";
import type { ContentCard } from "@shared/ui/content-card.js";

// The stylesheet is read from disk rather than through the `?inline`
// import: contract pins assert the served source, with comments stripped
// first so prose can never satisfy a pin (testing rule 7).
const searchStyles = stripCssComments(
  await Bun.file(new URL("./search-page.css", import.meta.url)).text(),
);

function stubFetch(
  handler: (
    url: string,
    init?: RequestInit,
  ) => Promise<{ status: number; body: unknown }>,
) {
  mockFetchImpl(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input.toString();
    const res = await handler(url, init);
    return mockResponse({
      ok: res.status < 400,
      status: res.status,
      headers:
        res.status >= 400 ? { "Content-Type": "application/problem+json" } : {},
      jsonBody: res.body,
    });
  });
}

interface Hit {
  type: "post" | "project";
  id: string;
  title: string;
  subtitle: string;
  description: string;
  thumbnailUrl: string | null;
  publishedAt: string;
  commentCount: number;
  favoriteCount: number;
}

function pageBody(
  items: Hit[],
  hasNextPage: boolean,
  endCursor: string | null,
  total = items.length,
) {
  return { items, pageInfo: { hasNextPage, endCursor, total } };
}

function problemBody(status: number) {
  return {
    type: "/problems/internal-error",
    title: "X",
    status,
    requestId: "rid",
  };
}

/** One minimal result row. */
function hitItem(type: "post" | "project", id: string, title: string): Hit {
  return {
    type,
    id,
    title,
    subtitle: "",
    description: "",
    thumbnailUrl: null,
    publishedAt: "2026-08-15T18:30:00.000Z",
    commentCount: 0,
    favoriteCount: 0,
  };
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
  restoreMockFetch();
  document.body.innerHTML = "";
});

async function renderPage(search = ""): Promise<SearchPage> {
  setHappyDOMURL(`https://example.com/search${search}`);
  const page = document.createElement("search-page");
  document.body.appendChild(page);
  await flush(page);
  return page;
}

async function flush(page: SearchPage) {
  await new Promise((r) => setTimeout(r, 0));
  await page.updateComplete;
}

function root(page: SearchPage): HTMLElement {
  return page.shadowRoot! as unknown as HTMLElement;
}

/** The rendered result cards, in document order. */
function cards(page: SearchPage): ContentCard[] {
  return [...root(page).querySelectorAll("content-card")] as ContentCard[];
}

/** A card's own shadow root — the card renders its content there. */
function cardRoot(card: ContentCard): HTMLElement {
  return card.shadowRoot! as unknown as HTMLElement;
}

/** The shared pager-nav's shadow root (rendered by the success state). */
function pagerRoot(page: SearchPage): HTMLElement {
  const pager = root(page).querySelector("pager-nav");
  if (!pager) throw new Error("pager-nav not rendered");
  return pager.shadowRoot as unknown as HTMLElement;
}

/** The pager's chip buttons in DOM order: [Previous, Next]. */
function pagerButtons(page: SearchPage): HTMLButtonElement[] {
  return [...pagerRoot(page).querySelectorAll("button")] as HTMLButtonElement[];
}

function text(root: HTMLElement, selector: string): string {
  return (root.querySelector(selector)?.textContent ?? "")
    .replace(/\s+/g, " ")
    .trim();
}

describe("search-page", () => {
  test("the submit control names its own action and drops to a full-width row on phones", async () => {
    const page = await renderPage();
    const submit = root(page).querySelector(".submit") as HTMLButtonElement;
    expect(submit.getAttribute("type")).toBe("submit");
    expect(submit.textContent?.trim()).toBe(el.search.submit);
    expect(el.search.submit).not.toBe(el.nav.search);

    // happy-dom cannot evaluate media queries: the phone-band placement is
    // pinned against the CSS source.
    const phone =
      searchStyles.match(/@media \(max-width: 700px\) \{(.*?)\n\}/s)?.[1] ?? "";
    const row = phone.match(/\.search-row \{([^}]*)\}/)?.[1] ?? "";
    expect(row).toContain("flex-wrap: wrap");
    const button = phone.match(/\.submit \{([^}]*)\}/)?.[1] ?? "";
    expect(button).toContain("flex: 1 0 100%");
  });

  test("renders the indicator stats on results — one chip, both indicators always", async () => {
    const hits: Hit[] = [
      {
        type: "post",
        id: "b1",
        title: "Ανάρτηση",
        subtitle: "",
        description: "",
        thumbnailUrl: null,
        publishedAt: "2026-08-15T18:30:00.000Z",
        commentCount: 5,
        favoriteCount: 0,
      },
      {
        type: "project",
        id: "p1",
        title: "Έργο",
        subtitle: "",
        description: "",
        thumbnailUrl: null,
        publishedAt: "2026-08-15T18:30:00.000Z",
        commentCount: 1,
        favoriteCount: 2,
      },
    ];
    stubFetch(async () => ({
      status: 200,
      body: pageBody(hits, false, null),
    }));

    const page = await renderPage("?q=test");

    const [post, project] = cards(page);
    // The counts ride the card hosts' inputs...
    expect(post?.commentCount).toBe(5);
    expect(post?.favoriteCount).toBe(0);
    expect(project?.commentCount).toBe(1);
    expect(project?.favoriteCount).toBe(2);

    // ...and both indicators ALWAYS render — zeros included: the card's
    // chip omits itself only when the page sets no counts at all.
    const postStats = cardRoot(post!).querySelectorAll(".content-stats .stat");
    expect(postStats.length).toBe(2);
    expect(postStats[0]?.getAttribute("aria-label")).toBe("5 σχόλια");
    expect(postStats[1]?.getAttribute("aria-label")).toBe("0 αγαπημένα");
    // Project hit: both indicators with the singular comment label.
    const projectStats = cardRoot(project!).querySelectorAll(
      ".content-stats .stat",
    );
    expect(projectStats.length).toBe(2);
    expect(projectStats[1]?.getAttribute("aria-label")).toBe("2 αγαπημένα");
    page.remove();
  });

  test("renders the prompt state when no query is present", async () => {
    stubFetch(async () => {
      throw new Error("must not fetch");
    });

    const page = await renderPage();
    expect(text(root(page), ".empty")).toBe(el.search.prompt);
    expect(root(page).querySelector("form")).not.toBeNull();
    // The reserved-footprint wrapper keeps the search form from moving when
    // the state changes.
    expect(
      root(page).querySelector(".empty")?.closest(".state-block"),
    ).not.toBeNull();
    page.remove();
  });

  test("reads q and type from the URL on mount", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody([], false, null) };
    });

    const page = await renderPage("?q=one%20piece&type=projects");
    expect(urls[0]).toBe("/api/v1/search?q=one+piece&type=projects&limit=10");
    expect(text(root(page), ".empty")).toBe(el.search.empty);
    page.remove();
  });

  test("submitting the form runs the search and renders merged results with badges", async () => {
    const hits: Hit[] = [
      {
        type: "project",
        id: "p1",
        title: "One Piece",
        subtitle: "",
        description: "Έργο",
        thumbnailUrl: "https://example.com/p.jpg",
        publishedAt: "2026-08-15T18:30:00.000Z",
        commentCount: 0,
        favoriteCount: 0,
      },
      {
        type: "post",
        id: "b1",
        title: "Ανάρτηση",
        subtitle: "Επεισόδιο 1.026",
        description: "Κείμενο",
        thumbnailUrl: null,
        publishedAt: "2026-08-14T18:30:00.000Z",
        commentCount: 0,
        favoriteCount: 0,
      },
    ];
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody(hits, false, null) };
    });

    const page = await renderPage();
    const input = root(page).querySelector("input[name=q]") as HTMLInputElement;
    input.value = "one piece";
    root(page)
      .querySelector("form")!
      .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await flush(page);

    expect(urls).toEqual(["/api/v1/search?q=one+piece&limit=10"]);
    expect(window.location.search).toBe("?q=one+piece");

    // Each hit renders as one roomy content-card row — the page wires the
    // card's inputs; the card's own suite pins its rendering.
    const results = cards(page);
    expect(results.length).toBe(2);
    expect(results.every((card) => card.shape === "row")).toBe(true);
    expect(results.every((card) => card.headingLevel === 2)).toBe(true);
    expect(results.map((card) => card.href)).toEqual([
      "/projects/p1",
      "/blog/b1",
    ]);
    expect(results[0]?.title).toBe("One Piece");
    expect(results[0]?.description).toBe("Έργο");
    // The page wires the subtitle input; the card renders it inline beside
    // the title (the card's own suite pins that rendering).
    expect(results[1]?.subtitle).toBe("Επεισόδιο 1.026");
    // A project hit has no subtitle, so its card renders the plain title.
    expect(cardRoot(results[0]!).querySelector(".title-line")).toBeNull();
    expect(results[0]?.metaInstant).toBe("2026-08-15T18:30:00.000Z");
    expect(results[0]?.badgeTone).toBe("project");
    expect(results[1]?.badgeTone).toBe("accent");

    // Project badge is the Projects anglicism inside lang="en"; the post
    // badge is Greek "Αναρτήσεις".
    const projectBadge = cardRoot(results[0]!).querySelector(".badge");
    expect(
      projectBadge?.querySelector('span[lang="en"]')?.textContent?.trim(),
    ).toBe(el.search.typeProjects);
    expect(projectBadge?.getAttribute("data-tone")).toBe("project");
    expect(results[1]?.badge).toBe(el.search.typePosts);

    // The whole row is one anchor to its detail route, and the description
    // renders plain (the row shape carries no quote glyphs).
    expect(
      cardRoot(results[0]!).querySelector("a.frame")?.getAttribute("href"),
    ).toBe("/projects/p1");
    expect(text(cardRoot(results[0]!), ".description")).toBe("Έργο");

    // The thumbnail input renders its image; a null one renders no img.
    expect(results[0]?.thumbnailUrl).toBe("https://example.com/p.jpg");
    expect(
      cardRoot(results[0]!).querySelector("img.thumb")?.getAttribute("src"),
    ).toBe("https://example.com/p.jpg");
    expect(cardRoot(results[1]!).querySelector("img.thumb")).toBeNull();

    page.remove();
  });

  test("changing the type re-runs the search with the same q", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody([], false, null) };
    });

    const page = await renderPage("?q=one%20piece");
    const postsRadio = root(page).querySelector(
      "input[name=type][value=posts]",
    ) as HTMLInputElement;
    postsRadio.checked = true;
    postsRadio.dispatchEvent(new Event("change", { bubbles: true }));
    await flush(page);

    expect(urls).toEqual([
      "/api/v1/search?q=one+piece&limit=10",
      "/api/v1/search?q=one+piece&type=posts&limit=10",
    ]);
    expect(window.location.search).toBe("?q=one+piece&type=posts");
    page.remove();
  });

  test("renders the empty state when the search has no hits", async () => {
    stubFetch(async () => ({ status: 200, body: pageBody([], false, null) }));

    const page = await renderPage("?q=ανύπαρκτο");
    expect(text(root(page), ".empty")).toBe(el.search.empty);
    page.remove();
  });

  test("renders the error state with a retry that reloads", async () => {
    let calls = 0;
    stubFetch(async () => {
      calls++;
      if (calls === 1) return { status: 500, body: problemBody(500) };
      return { status: 200, body: pageBody([], false, null) };
    });

    const page = await renderPage("?q=test");
    expect(root(page).querySelector("error-banner")).not.toBeNull();

    root(page)
      .querySelector(".retry")!
      .dispatchEvent(new Event("click", { bubbles: true }));
    await flush(page);
    expect(text(root(page), ".empty")).toBe(el.search.empty);
    page.remove();
  });

  test("next pages through the URL-carried cursor; previous goes back", async () => {
    const hit = (id: string): Hit => ({
      type: "post",
      id,
      title: id,
      subtitle: "",
      description: "",
      thumbnailUrl: null,
      publishedAt: "2026-08-15T18:30:00.000Z",
      commentCount: 0,
      favoriteCount: 0,
    });
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return url.includes("after=")
        ? { status: 200, body: pageBody([hit("p2")], false, null) }
        : { status: 200, body: pageBody([hit("p1")], true, "sv1.abc") };
    });

    const page = await renderPage("\?q=test");
    // Page 1: Previous disabled (no marker in this session), Next live.
    expect(pagerButtons(page).length).toBe(2);
    expect(pagerButtons(page)[0]?.disabled).toBe(true);
    expect(pagerButtons(page)[1]?.disabled).toBe(false);

    // Next: the continuation cursor replaces load-more — it rides the URL.
    pagerButtons(page)[1]!.dispatchEvent(new Event("click", { bubbles: true }));
    await flush(page);

    expect(urls[1]).toBe("/api/v1/search?q=test&limit=10&after=sv1.abc");
    expect(window.location.search).toBe(
      "?q=test&limit=10&after=sv1.abc&page=2",
    );
    // Results are REPLACED, not appended (page-based, not load-more).
    expect(cards(page).length).toBe(1);
    // Previous is now enabled (hasNextPage=false disables Next).
    const buttons = pagerButtons(page);
    expect(buttons.length).toBe(2);
    expect(buttons[0]?.disabled).toBe(false);
    expect(buttons[1]?.disabled).toBe(true);
    // The page-turn chips are arrow-only; the catalog words moved to the
    // accessible name.
    expect(buttons[0]?.getAttribute("aria-label")).toBe(el.ui.prevPage);

    page.remove();
  });

  test("mounting directly on a cursor page disables Previous until next is used", async () => {
    const hit: Hit = {
      type: "post",
      id: "p9",
      title: "p9",
      subtitle: "",
      description: "",
      thumbnailUrl: null,
      publishedAt: "2026-08-15T18:30:00.000Z",
      commentCount: 0,
      favoriteCount: 0,
    };
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody([hit], false, null) };
    });

    // A shared/deep-linked page-2 URL: the fetch must carry the cursor,
    // and Previous must stay DISABLED (no sfPager marker — history.back()
    // would leave the app). Next is disabled too (hasNextPage=false).
    const page = await renderPage("\?q=test&after=sv1.abc");
    expect(urls[0]).toBe("/api/v1/search?q=test&limit=10&after=sv1.abc");
    expect(pagerButtons(page)[0]?.disabled).toBe(true);
    expect(pagerButtons(page)[1]?.disabled).toBe(true);
    page.remove();
  });

  test("after a refresh on a cursor page, Previous stays available (the state marker persists)", async () => {
    const hit: Hit = {
      type: "post",
      id: "p1",
      title: "p1",
      subtitle: "",
      description: "",
      thumbnailUrl: null,
      publishedAt: "2026-08-15T18:30:00.000Z",
      commentCount: 0,
      favoriteCount: 0,
    };
    stubFetch(async (url) =>
      url.includes("after=")
        ? { status: 200, body: pageBody([hit], false, null) }
        : { status: 200, body: pageBody([hit], true, "sv1.abc") },
    );

    const page = await renderPage("\?q=test");
    pagerButtons(page)[1]!.dispatchEvent(new Event("click", { bubbles: true }));
    await flush(page);
    expect(pagerButtons(page)[0]?.disabled).toBe(false); // previous live
    page.remove();

    // "Refresh": a NEW page instance mounts on the same cursor URL.
    // Session-history state survives a reload, so the sfPager marker does
    // too — Previous must remain enabled.
    const refreshed = await renderPage("\?q=test&limit=10&after=sv1.abc");
    expect(pagerButtons(refreshed)[0]?.disabled).toBe(false);
    expect(pagerButtons(refreshed)[1]?.disabled).toBe(true);
  });

  test("changing the page size restarts at the first page with the new size", async () => {
    const hit: Hit = {
      type: "post",
      id: "p1",
      title: "p1",
      subtitle: "",
      description: "",
      thumbnailUrl: null,
      publishedAt: "2026-08-15T18:30:00.000Z",
      commentCount: 0,
      favoriteCount: 0,
    };
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody([hit], false, null) };
    });

    const page = await renderPage("\?q=test");
    const select = pagerRoot(page).querySelector("select") as HTMLSelectElement;
    select.value = "20";
    select.dispatchEvent(new Event("change", { bubbles: true }));
    await flush(page);

    // The size rides the URL; the cursor is dropped (a cursor is only
    // meaningful with the same limit) — the first page reloads at size 20.
    expect(window.location.search).toBe("?q=test&limit=20");
    expect(urls[1]).toBe("/api/v1/search?q=test&limit=20");
    page.remove();
  });

  test("a failing page-2 load renders the error state with retry", async () => {
    const hit: Hit = {
      type: "post",
      id: "p1",
      title: "p1",
      subtitle: "",
      description: "",
      thumbnailUrl: null,
      publishedAt: "2026-08-15T18:30:00.000Z",
      commentCount: 0,
      favoriteCount: 0,
    };
    let calls = 0;
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      calls++;
      if (url.includes("after=")) {
        // First page-2 attempt fails; the retry succeeds.
        if (calls === 2) return { status: 500, body: problemBody(500) };
        return { status: 200, body: pageBody([hit], false, null) };
      }
      return { status: 200, body: pageBody([hit], true, "sv1.abc") };
    });

    const page = await renderPage("\?q=test");
    pagerButtons(page)[1]!.dispatchEvent(new Event("click", { bubbles: true }));
    await flush(page);

    // The failed page turn is the same failure path as any load: the
    // error banner + retry (at least one failure path per interaction).
    // The pager is hidden on the error state. The URL still
    // carries the cursor, so retry reloads the SAME page.
    expect(root(page).querySelector("error-banner")).not.toBeNull();
    expect(root(page).querySelector("pager-nav")).toBeNull();
    root(page)
      .querySelector(".retry")!
      .dispatchEvent(new Event("click", { bubbles: true }));
    await flush(page);
    expect(cards(page).length).toBe(1);
    expect(urls[2]).toBe("/api/v1/search?q=test&limit=10&after=sv1.abc");
    page.remove();
  });

  test("re-syncs from the URL when a navigation targets /search", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody([], false, null) };
    });

    const page = await renderPage();
    // SPA navigation landing on /search from another route: the shell
    // pushes the URL first, then the event fires.
    window.history.pushState({}, "", "/search?q=καλημέρα&sort=title");
    window.dispatchEvent(
      new CustomEvent(NAVIGATE_EVENT, {
        detail: { path: "/search?q=καλημέρα&sort=title" },
      }),
    );
    await flush(page);

    expect(urls[0]).toBe(
      "/api/v1/search?q=%CE%BA%CE%B1%CE%BB%CE%B7%CE%BC%CE%AD%CF%81%CE%B1&sort=title&limit=10",
    );
    // The sort travels with the incoming URL too (the popstate path).
    expect(
      (root(page).querySelector('select[name="sort"]') as HTMLSelectElement)
        .value,
    ).toBe("title");
    page.remove();
  });

  test("reads the sort from the URL and sends it with the search", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody([], false, null) };
    });

    const page = await renderPage("?q=test&sort=title");
    expect(urls).toEqual(["/api/v1/search?q=test&sort=title&limit=10"]);
    // The select mirrors the URL (happy-dom needs the imperative sync).
    const select = root(page).querySelector(
      'select[name="sort"]',
    ) as HTMLSelectElement;
    expect(select.value).toBe("title");
    expect(text(root(page), ".sort-label")).toBe(el.search.sortLabel);
    page.remove();
  });

  test("reads the oldest sort from the URL and sends it with the search", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody([], false, null) };
    });

    const page = await renderPage("?q=test&sort=oldest");
    expect(urls).toEqual(["/api/v1/search?q=test&sort=oldest&limit=10"]);
    const select = root(page).querySelector(
      'select[name="sort"]',
    ) as HTMLSelectElement;
    expect(select.value).toBe("oldest");
    page.remove();
  });

  test("changing the sort re-runs at the first page and rides the URL", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody([], false, null) };
    });

    // Mounted on page 2: a sort change must drop the cursor (a cursor is
    // only meaningful with the q+type+sort that produced it).
    const page = await renderPage("?q=test&limit=10&after=sv1.abc");
    const select = root(page).querySelector(
      'select[name="sort"]',
    ) as HTMLSelectElement;
    select.value = "title";
    select.dispatchEvent(new Event("change", { bubbles: true }));
    await flush(page);

    expect(window.location.search).toBe("?q=test&sort=title");

    // Switching back to the default drops the parameter entirely.
    select.value = "date";
    select.dispatchEvent(new Event("change", { bubbles: true }));
    await flush(page);
    expect(window.location.search).toBe("?q=test");
    expect(urls).toEqual([
      "/api/v1/search?q=test&limit=10&after=sv1.abc",
      "/api/v1/search?q=test&sort=title&limit=10",
      "/api/v1/search?q=test&limit=10",
    ]);
    page.remove();
  });

  test("the sort option labels come from the catalog", async () => {
    stubFetch(async () => ({ status: 200, body: pageBody([], false, null) }));

    const page = await renderPage("?q=test");
    const options = [
      ...root(page).querySelectorAll('select[name="sort"] option'),
    ].map((o) => o.textContent?.trim());
    expect(options).toEqual([
      el.search.sortDate,
      el.search.sortOldest,
      el.search.sortTitle,
    ]);
    page.remove();
  });

  test("the date window rides the URL and the request", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody([], false, null) };
    });

    // Mounted on page 2: a window change is a refinement, so it restarts at
    // the first page (the cursor is dropped).
    const page = await renderPage(
      "?q=test&limit=10&after=sv1.abc&from=2025-01-01",
    );
    const from = root(page).querySelector(
      'input[name="from"]',
    ) as HTMLInputElement;
    const to = root(page).querySelector('input[name="to"]') as HTMLInputElement;
    expect(from.value).toBe("2025-01-01");
    expect(to.value).toBe("");

    to.value = "2025-01-31";
    to.dispatchEvent(new Event("change", { bubbles: true }));
    await flush(page);

    expect(window.location.search).toBe(
      "?q=test&from=2025-01-01&to=2025-01-31",
    );

    // Clearing a bound drops the parameter entirely.
    from.value = "";
    from.dispatchEvent(new Event("change", { bubbles: true }));
    await flush(page);
    expect(window.location.search).toBe("?q=test&to=2025-01-31");
    expect(urls).toEqual([
      "/api/v1/search?q=test&from=2025-01-01&limit=10&after=sv1.abc",
      "/api/v1/search?q=test&from=2025-01-01&to=2025-01-31&limit=10",
      "/api/v1/search?q=test&to=2025-01-31&limit=10",
    ]);
    page.remove();
  });

  test("the date controls carry catalog labels and ignore non-canonical URL values", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody([], false, null) };
    });

    const page = await renderPage("?q=test&from=01-01-2025");
    const labels = [...root(page).querySelectorAll(".date-control > span")].map(
      (s) => s.textContent?.trim(),
    );
    expect(labels).toEqual([el.search.dateFrom, el.search.dateTo]);
    // The group carries a context label for assistive tech.
    expect(text(root(page), ".date-row legend")).toBe(el.search.dateLabel);
    // A non-canonical bound never reaches the input or the request (the
    // server would reject it with a 422).
    expect(
      (root(page).querySelector('input[name="from"]') as HTMLInputElement)
        .value,
    ).toBe("");
    expect(urls[0]).toBe("/api/v1/search?q=test&limit=10");
    page.remove();
  });

  test("a calendar-impossible or pre-1970 URL bound is ignored too", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody([], false, null) };
    });

    const page = await renderPage("?q=test&from=2025-02-30&to=1969-12-31");
    expect(
      (root(page).querySelector('input[name="from"]') as HTMLInputElement)
        .value,
    ).toBe("");
    expect(
      (root(page).querySelector('input[name="to"]') as HTMLInputElement).value,
    ).toBe("");
    expect(urls[0]).toBe("/api/v1/search?q=test&limit=10");
    page.remove();
  });

  test("next keeps the sort and the window in the URL", async () => {
    const hit: Hit = {
      type: "post",
      id: "b1",
      title: "b1",
      subtitle: "",
      description: "",
      thumbnailUrl: null,
      publishedAt: "2025-01-15T18:30:00.000Z",
      commentCount: 0,
      favoriteCount: 0,
    };
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return url.includes("after=")
        ? { status: 200, body: pageBody([hit], false, null) }
        : { status: 200, body: pageBody([hit], true, "st1.abc") };
    });

    const page = await renderPage(
      "?q=test&sort=title&from=2025-01-01&to=2025-01-31",
    );
    pagerButtons(page)[1]!.dispatchEvent(new Event("click", { bubbles: true }));
    await flush(page);

    expect(window.location.search).toBe(
      "?q=test&sort=title&from=2025-01-01&to=2025-01-31&limit=10&after=st1.abc&page=2",
    );
    expect(urls[1]).toBe(
      "/api/v1/search?q=test&sort=title&from=2025-01-01&to=2025-01-31&limit=10&after=st1.abc",
    );
    page.remove();
  });

  test("a date change on an idle page keeps the pick in the URL and fetches nothing", async () => {
    stubFetch(async () => {
      throw new Error("must not fetch");
    });

    const page = await renderPage();
    const from = root(page).querySelector(
      'input[name="from"]',
    ) as HTMLInputElement;
    from.value = "2025-01-01";
    from.dispatchEvent(new Event("change", { bubbles: true }));
    await flush(page);
    expect(window.location.search).toBe("?from=2025-01-01");

    // Clearing it again returns to the BARE path — no dangling `?` when
    // every refinement is back at its default.
    from.value = "";
    from.dispatchEvent(new Event("change", { bubbles: true }));
    await flush(page);
    expect(window.location.search).toBe("");
    expect(window.location.pathname).toBe("/search");

    expect(text(root(page), ".empty")).toBe(el.search.prompt);
    page.remove();
  });

  test("a picked date survives until the query is submitted", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody([], false, null) };
    });

    const page = await renderPage();
    const from = root(page).querySelector(
      'input[name="from"]',
    ) as HTMLInputElement;
    from.value = "2025-01-01";
    from.dispatchEvent(new Event("change", { bubbles: true }));
    await flush(page);
    expect(urls).toHaveLength(0);

    // Submitting the query now carries the picked window (the refinement was
    // never discarded) and runs the search once.
    const input = root(page).querySelector(
      'input[name="q"]',
    ) as HTMLInputElement;
    input.value = "test";
    root(page)
      .querySelector("form")!
      .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await flush(page);

    expect(window.location.search).toBe("?q=test&from=2025-01-01");
    expect(urls).toHaveLength(1);
    expect(urls[0]).toBe("/api/v1/search?q=test&from=2025-01-01&limit=10");
    page.remove();
  });

  test("the picker's input+change pair issues ONE request", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody([], false, null) };
    });

    const page = await renderPage("\?q=test");
    await flush(page);
    urls.length = 0; // the initial page load is not part of this pin

    const from = root(page).querySelector(
      'input[name="from"]',
    ) as HTMLInputElement;
    // Browsers fire the pair for one picker selection; whichever order they
    // arrive in, the second is a no-op because it carries the applied value.
    from.value = "2025-01-01";
    from.dispatchEvent(new Event("input", { bubbles: true }));
    from.dispatchEvent(new Event("change", { bubbles: true }));
    await flush(page);

    expect(urls).toEqual(["/api/v1/search?q=test&from=2025-01-01&limit=10"]);
    page.remove();
  });

  test("a refinement picked while idle survives into the next search's every URL builder", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return {
        status: 200,
        // The title sort's cursor namespace — the sort is what the request
        // below rides, so the fixture must carry it.
        body: pageBody([hitItem("post", "p1", "A")], true, "st1.abc"),
      };
    });

    const page = await renderPage();
    const select = root(page).querySelector(
      'select[name="sort"]',
    ) as HTMLSelectElement;
    select.value = "title";
    select.dispatchEvent(new Event("change", { bubbles: true }));
    await flush(page);
    expect(window.location.search).toBe("?sort=title");
    expect(urls).toHaveLength(0);

    // The sort picked before the search still rides Next (every URL builder
    // reads the state onLoad wrote).
    const input = root(page).querySelector(
      'input[name="q"]',
    ) as HTMLInputElement;
    input.value = "test";
    root(page)
      .querySelector("form")!
      .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await flush(page);

    const pager = pagerButtons(page);
    expect(pager).toHaveLength(2);
    pager[1]!.dispatchEvent(new Event("click", { bubbles: true }));
    await flush(page);

    expect(window.location.search).toBe(
      "?q=test&sort=title&limit=10&after=st1.abc&page=2",
    );
    expect(urls[1]).toBe(
      "/api/v1/search?q=test&sort=title&limit=10&after=st1.abc",
    );
    page.remove();
  });

  test("a type picked on an idle page rides the URL and fetches nothing", async () => {
    stubFetch(async () => {
      throw new Error("must not fetch");
    });

    const page = await renderPage();
    const projects = root(page).querySelector(
      'input[name="type"][value="projects"]',
    ) as HTMLInputElement;
    projects.checked = true;
    projects.dispatchEvent(new Event("change", { bubbles: true }));
    await flush(page);

    expect(window.location.search).toBe("?type=projects");
    expect(text(root(page), ".empty")).toBe(el.search.prompt);
    // The control reflects the URL, not a local toggle: the radio stays
    // checked through the re-render.
    expect(
      (
        root(page).querySelector(
          'input[name="type"][value="projects"]',
        ) as HTMLInputElement
      ).checked,
    ).toBe(true);
    page.remove();
  });

  test("submitting an empty query stays on the idle state", async () => {
    stubFetch(async () => {
      throw new Error("must not fetch");
    });

    const page = await renderPage();
    root(page)
      .querySelector("form")!
      .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await flush(page);

    expect(text(root(page), ".empty")).toBe(el.search.prompt);
    page.remove();
  });

  test("the clear button resets the query back to the idle state", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody([], false, null) };
    });

    const page = await renderPage("\?q=test");
    // The clear (×) button appears once a query is active, with the
    // catalog aria-label.
    const clearBtn = root(page).querySelector(
      ".clear-btn",
    ) as HTMLButtonElement | null;
    expect(clearBtn).not.toBeNull();
    expect(clearBtn?.getAttribute("aria-label")).toBe(el.search.clear);

    clearBtn!.dispatchEvent(new Event("click", { bubbles: true }));
    await flush(page);

    // URL is bare /search again and the page shows the prompt — no
    // further fetch.
    expect(window.location.search).toBe("");
    expect(text(root(page), ".empty")).toBe(el.search.prompt);
    expect(root(page).querySelector(".clear-btn")).toBeNull();
    expect(urls.length).toBe(1);
    // The control that held focus is removed by the re-render; focus lands
    // on the query input instead.
    expect(page.shadowRoot?.activeElement).toBe(
      root(page).querySelector('input[name="q"]'),
    );
    page.remove();
  });

  test("a typed draft gets the clear control before it is submitted", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody([], false, null) };
    });

    const page = await renderPage();
    expect(root(page).querySelector(".clear-btn")).toBeNull();

    const input = root(page).querySelector(
      'input[name="q"]',
    ) as HTMLInputElement;
    input.value = "draft";
    input.dispatchEvent(new Event("input", { bubbles: true }));
    await flush(page);

    // The draft survives the re-render the flag triggers.
    expect(input.value).toBe("draft");
    const clearBtn = root(page).querySelector(
      ".clear-btn",
    ) as HTMLButtonElement | null;
    expect(clearBtn).not.toBeNull();
    expect(clearBtn?.getAttribute("aria-label")).toBe(el.search.clear);

    clearBtn!.dispatchEvent(new Event("click", { bubbles: true }));
    await flush(page);

    // The draft is gone, the URL is the bare path, and nothing was fetched
    // — the control stays submit-driven.
    expect(
      (root(page).querySelector('input[name="q"]') as HTMLInputElement).value,
    ).toBe("");
    expect(window.location.search).toBe("");
    expect(urls).toEqual([]);
    page.remove();
  });

  test("a draft survives refinement renders and the × follows the field", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody([], false, null) };
    });

    const page = await renderPage();
    const input = root(page).querySelector(
      'input[name="q"]',
    ) as HTMLInputElement;
    input.value = "draft";
    input.dispatchEvent(new Event("input", { bubbles: true }));
    await flush(page);
    expect(root(page).querySelector(".clear-btn")).not.toBeNull();

    // A refinement while the page is idle re-renders but must neither
    // clobber the draft nor drop the ×.
    const select = root(page).querySelector(
      'select[name="sort"]',
    ) as HTMLSelectElement;
    select.value = "title";
    select.dispatchEvent(new Event("change", { bubbles: true }));
    await flush(page);
    expect(window.location.search).toBe("?sort=title");
    expect(input.value).toBe("draft");
    expect(root(page).querySelector(".clear-btn")).not.toBeNull();

    // Emptying the field removes the × without touching the URL.
    input.value = "";
    input.dispatchEvent(new Event("input", { bubbles: true }));
    await flush(page);
    expect(root(page).querySelector(".clear-btn")).toBeNull();
    expect(window.location.search).toBe("?sort=title");
    expect(urls).toEqual([]);
    page.remove();
  });

  test("a URL q beyond the wire bound is ignored", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody([], false, null) };
    });

    const page = await renderPage(`?q=${"x".repeat(101)}`);

    // No request — the server would 422 the bound and no retry could fix it.
    expect(urls).toEqual([]);
    expect(text(root(page), ".empty")).toBe(el.search.prompt);
    expect(
      (root(page).querySelector('input[name="q"]') as HTMLInputElement).value,
    ).toBe("");
    page.remove();
  });

  test("a URL q at the wire bound is accepted, counted in runes", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody([], false, null) };
    });

    const hundred = "x".repeat(100);
    const page = await renderPage(`?q=${hundred}`);
    expect(urls).toHaveLength(1);
    expect(urls[0]).toContain(`q=${hundred}`);

    // 100 astral runes are 200 UTF-16 code units; a code-unit count would
    // wrongly reject them.
    const astral = "😀".repeat(100);
    const second = await renderPage(`?q=${encodeURIComponent(astral)}`);
    expect(urls).toHaveLength(2);
    expect(urls[1]).toContain(`q=${encodeURIComponent(astral)}`);
    page.remove();
    second.remove();
  });

  test("a pick the canonical reader rejects is cleared instead of riding the URL", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody([], false, null) };
    });

    const page = await renderPage("?q=test");
    urls.length = 0;

    const from = root(page).querySelector(
      'input[name="from"]',
    ) as HTMLInputElement;
    from.value = "1969-12-31";
    from.dispatchEvent(new Event("change", { bubbles: true }));
    await flush(page);

    // The rejected bound leaves the field and the URL together; the request
    // list is unchanged (the bound was already absent).
    expect(from.value).toBe("");
    expect(window.location.search).toBe("?q=test");
    expect(urls).toEqual([]);
    page.remove();
  });

  test("a rejected pick drops a previously applied bound from the URL", async () => {
    const urls: string[] = [];
    stubFetch(async (url) => {
      urls.push(url);
      return { status: 200, body: pageBody([], false, null) };
    });

    const page = await renderPage("?q=test&from=2025-01-01");
    urls.length = 0;

    const from = root(page).querySelector(
      'input[name="from"]',
    ) as HTMLInputElement;
    from.value = "1969-12-31";
    from.dispatchEvent(new Event("change", { bubbles: true }));
    await flush(page);

    // The old bound leaves the URL with the rejected pick, and one reload
    // re-issues the query without it.
    expect(from.value).toBe("");
    expect(window.location.search).toBe("?q=test");
    expect(urls).toEqual(["/api/v1/search?q=test&limit=10"]);
    page.remove();
  });

  test("a newer query supersedes the in-flight load", async () => {
    const signals: (AbortSignal | null)[] = [];
    let releaseFirst: (() => void) | null = null;
    stubFetch(async (url, init) => {
      signals.push(init?.signal ?? null);
      if (url.includes("q=first")) {
        await new Promise<void>((resolve) => {
          releaseFirst = resolve;
        });
        return {
          status: 200,
          body: pageBody([hitItem("post", "b1", "old")], false, null),
        };
      }
      return {
        status: 200,
        body: pageBody([hitItem("post", "b2", "new")], false, null),
      };
    });

    const page = await renderPage("?q=first");
    const input = root(page).querySelector(
      'input[name="q"]',
    ) as HTMLInputElement;
    input.value = "second";
    root(page)
      .querySelector("form")!
      .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await flush(page);

    expect(signals).toHaveLength(2);
    expect(signals[0]?.aborted).toBe(true);
    expect(cards(page).map((card) => card.title)).toEqual(["new"]);

    // The superseded response resolves late and must be discarded.
    releaseFirst!();
    await flush(page);
    expect(cards(page).map((card) => card.title)).toEqual(["new"]);
    page.remove();
  });

  test("teardown aborts the in-flight load", async () => {
    const signals: (AbortSignal | null)[] = [];
    let release: (() => void) | null = null;
    stubFetch(async (_url, init) => {
      signals.push(init?.signal ?? null);
      await new Promise<void>((resolve) => {
        release = resolve;
      });
      return { status: 200, body: pageBody([], false, null) };
    });

    const page = await renderPage("?q=test");
    expect(signals).toHaveLength(1);

    page.remove();
    expect(signals[0]?.aborted).toBe(true);

    // Let the abandoned load settle so nothing dangles past the test.
    release!();
    await flush(page);
  });

  test("the form carries its landmarks and control names", async () => {
    stubFetch(async () => {
      throw new Error("must not fetch");
    });

    const page = await renderPage();
    expect(root(page).querySelector("form")?.getAttribute("role")).toBe(
      "search",
    );
    expect(text(root(page), "h1")).toBe(el.titles.search);
    expect(
      root(page).querySelector('input[name="q"]')?.getAttribute("aria-label"),
    ).toBe(el.nav.search);
    expect(text(root(page), ".type-filter legend")).toBe(el.search.typeLabel);
    page.remove();
  });
});
